## Explaining code

### System prompt:

_You might want to view the markdown for all these prompts. I'll use xml tags in bold to denote the start/end of prompts, since they use markdown in the prompt. I'm also formatting these out with newlines to be more readable._

*<System prompt>*

You are an intelligent programmer, powered by GPT-3.5. You are happy to help answer any questions that the user has (usually they will be about coding).

1. Please keep your response as concise as possible, and avoid being too verbose.

2. When the user is asking for edits to their code, please output a simplified version of the code block that highlights the changes necessary and adds comments to indicate where unchanged code has been skipped. For example:

```file_path
// ... existing code ...
{{ edit_1 }}
// ... existing code ...
{{ edit_2 }}
// ... existing code ...
```

The user can see the entire file, so they prefer to only read the updates to the code. Often this will mean that the start/end of the file will be skipped, but that's okay! Rewrite the entire file only if specifically requested. Always provide a brief explanation of the updates, unless the user specifically requests only the code.

3. Do not lie or make up facts.

4. If a user messages you in a foreign language, please respond in that language.

5. Format your response in markdown.

6. When writing out new code blocks, please specify the language ID after the initial backticks, like so: 

```python
{{ code }}
```

7. When writing out code blocks for an existing file, please also specify the file path after the initial backticks and restate the method / class your codeblock belongs to, like so:

```typescript:app/components/Ref.tsx
function AIChatHistory() {
    ...
    {{ code }}
    ...
}
```

*</System prompt>*

Some initial takeaways:

- has the usual spiel about being concise and not printing the whole code

- pretty important - when using my own prompts the hardest part is getting the LLM to _not_ print out the whole file

- interesting format for editing code - you set the file context after the language name, and the function/class/block context in the code, and then the edits to that function or block.

### Files:

It looks like cursor sends another message with just the file context. Since this was a single-file request it's just the current file:

*<message>*
# Inputs

### Current File
Here is the file I'm looking at. It might be truncated from above and below and, if so, is centered around my cursor.
```main.go

*</message>*

There's an old twitter thread from the cursor founder (which I bookmarked, need to go find) about how they have this context-building library (which is now closed-source, funnily) that allows you to specify files as react components, setting their "priority" (and other params) using props. It then renders things, highest priority first, until your token budget is used up. I bet we start to see that as we look at larger and larger projects.

### The question

Finally, it puts the user's typed message or question in a third message all on its own. In this case, it was just

```
Explain this code
```


## Inspecting the binary

Luckily, cursor (like vscode) is just an electron app, so I can hunt through (albeit minified) JS to figure out what's going on. Turns out most requests it makes are RPCs of the form: `api{1,2,3,...}.cursor.sh/aiserver.v1.AiService/GetCompletion` - basically a service, followed by an RPC method name. I should try to de-minify that JS to figure out how I can set up an RPC client to connect to this service.

By un-minifying the code, the RPC structure is pretty clear. I'm going to try to write up an RPC client in go that can use these methods.

## Authentication

Press Login in cursor
In the url it opens, grab the uuid and verifier

Poll https://api2.cursor.sh/auth/poll?uuid=<uuid>&verifier=<verifier>
until you get a json response with `accessToken`, `refreshToken`, `challenge`, `authId`, `uuid`.

Should be able to build up an rpc client with these values?

To confirm connected, run:

```bash
curl -H "authorization: Bearer <accessToken>" -H "content-type: application/proto" -X POST "https://api2.cursor.sh/aiserver.v1.AuthService/GetEmail"
```

We should be using the extensionHostProcess.js file to build our protobuf message types. We only need certain `define` functions to run, so we should basically be building a registry of defines, and then working backwards from the define for `proto/aiserver/v1/aiserver_pb` until we have everything we need to get the protos. There's a lot of vscode cruft that we don't need.
