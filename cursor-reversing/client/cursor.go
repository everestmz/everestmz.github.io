package cursor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/everestmz/everestmz.github.io/cursor-reversing/client/cursor/gen/aiserver/v1/aiserverv1connect"
)

func generateChecksum(machineID string) string {
	// Get current timestamp and convert to uint64
	timestamp := uint64(time.Now().UnixNano() / 1e6)

	// Convert timestamp to 6-byte array
	timestampBytes := []byte{
		byte(timestamp >> 40),
		byte(timestamp >> 32),
		byte(timestamp >> 24),
		byte(timestamp >> 16),
		byte(timestamp >> 8),
		byte(timestamp),
	}

	// Apply rolling XOR encryption (function S in the original code)
	encryptedBytes := encryptBytes(timestampBytes)

	// Convert to base64
	base64Encoded := base64.StdEncoding.EncodeToString(encryptedBytes)

	// Concatenate with machineID
	return fmt.Sprintf("%s%s", base64Encoded, machineID)
}

func encryptBytes(input []byte) []byte {
	w := byte(165)
	for i := 0; i < len(input); i++ {
		input[i] = (input[i] ^ w) + byte(i%256)
		w = input[i]
	}
	return input
}

func NewRepositoryServiceClient() aiserverv1connect.RepositoryServiceClient {
	return aiserverv1connect.NewRepositoryServiceClient(
		http.DefaultClient,
		"https://repo42.cursor.sh",
	)
}

func NewAiServiceClient() aiserverv1connect.AiServiceClient {
	return aiserverv1connect.NewAiServiceClient(
		http.DefaultClient,
		BaseUrl(),
	)
}

func BaseUrl() string {
	return "https://api2.cursor.sh"
}

type AuthInfo struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	Challenge    string `json:"challenge"`
	AuthId       string `json:"authId"`
	Uuid         string `json:"uuid"`
}

func GetAuthJson() AuthInfo {
	info := AuthInfo{}

	configPath := os.ExpandEnv("$HOME/.config/cursor_client/auth.json")

	authContent, err := os.ReadFile(configPath)
	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(authContent, &info)
	if err != nil {
		panic(err)
	}

	return info
}

func NewRequest[T any](message *T) *connect.Request[T] {
	req := connect.NewRequest(message)

	authInfo := GetAuthJson()

	req.Header().Set("authorization", "bearer "+authInfo.AccessToken)
	req.Header().Set("x-cursor-client-version", "0.40.4")
	// It doesn't look like the checksum matters. Just that we need one?
	// Either way, this is the algorithm used. I just don't know what the arg is.
	req.Header().Set("x-cursor-checksum", generateChecksum("hi"))

	return req
}
