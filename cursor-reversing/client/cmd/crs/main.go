package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	cursor "github.com/everestmz/everestmz.github.io/cursor-reversing/client"
	aiserverv1 "github.com/everestmz/everestmz.github.io/cursor-reversing/client/cursor/gen/aiserver/v1"
	"github.com/spf13/cobra"
)

func init() {
	llmFlags := LlmCmd.PersistentFlags()
	llmFlags.StringP("model", "m", "", "Select a model to use")
}

var ModelsCmd = &cobra.Command{
	Use: "models",
	RunE: func(cmd *cobra.Command, args []string) error {
		aiClient := cursor.NewAiServiceClient()

		resp, err := aiClient.AvailableModels(context.TODO(), cursor.NewRequest(&aiserverv1.AvailableModelsRequest{}))
		if err != nil {
			return err
		}

		fmt.Println(resp.Msg.ModelNames)

		return nil
	},
}

// XXX: the login and poll commands don't work yet - still need to figure out what mechanism they're using for challenge/response verifier
var LoginCmd = &cobra.Command{
	Use: "login",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoClient := cursor.NewRepositoryServiceClient()

		loginResp, err := repoClient.LoginUser(context.TODO(), cursor.NewRequest(&aiserverv1.LoginRequest{}))
		if err != nil {
			return err
		}

		pollResp, err := repoClient.PollLoggedIn(context.TODO(), cursor.NewRequest(&aiserverv1.PollLoginRequest{}))
		if err != nil {
			return err
		}

		fmt.Println(pollResp.Msg.GetStatus())

		fmt.Println(loginResp.Msg.GetLoginUrl())

		return nil
	},
}

var PollLoginCmd = &cobra.Command{
	Use: "poll",
	RunE: func(cmd *cobra.Command, args []string) error {
		rawUrl := args[0]

		parsed, err := url.Parse(rawUrl)
		if err != nil {
			return err
		}

		state, err := url.QueryUnescape(parsed.Query()["state"][0])
		if err != nil {
			return err
		}

		stateMap := map[string]string{}
		err = json.Unmarshal([]byte(state), &stateMap)
		if err != nil {
			return err
		}

		returnUrl, err := url.Parse(stateMap["returnTo"])
		if err != nil {
			return err
		}

		loginUuid := returnUrl.Query()["uuid"][0]

		fmt.Println(loginUuid)

		for {
			time.Sleep(time.Second)
			resp, err := http.Get(fmt.Sprintf("https://api2.cursor.sh/auth/poll?uuid=%s", loginUuid))
			if err != nil {
				return err
			}

			fmt.Println(resp.Status)
		}

		return nil
	},
}

var LlmCmd = &cobra.Command{
	Use: "llm",
	RunE: func(cmd *cobra.Command, args []string) error {
		aiClient := cursor.NewAiServiceClient()

		flags := cmd.Flags()

		model, err := flags.GetString("model")
		if err != nil {
			return err
		}

		var modelPtr *string

		if model != "" {
			modelPtr = &model
		}

		resp, err := aiClient.StreamChat(context.TODO(), cursor.NewRequest(&aiserverv1.GetChatRequest{
			ModelDetails: &aiserverv1.ModelDetails{
				ModelName: modelPtr,
			},
			Conversation: []*aiserverv1.ConversationMessage{
				{
					Text: args[0],
					Type: aiserverv1.ConversationMessage_MESSAGE_TYPE_HUMAN,
				},
			},
		}))

		if err != nil {
			return err
		}

		for resp.Receive() {
			next := resp.Msg()
			fmt.Printf(next.Text)
		}

		return nil
	},
}

func main() {
	app := &cobra.Command{
		Use: os.Args[0],
	}

	app.AddCommand(LoginCmd, ModelsCmd, LlmCmd, PollLoginCmd)

	err := app.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
