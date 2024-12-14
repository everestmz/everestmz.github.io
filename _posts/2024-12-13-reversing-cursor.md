---
layout: post
title:  "Breaking VSCode’s chains: Reverse Engineering Cursor"
subtitle: Maybe I care too much about which tools I use... 
date:   2024-12-13
tags: cursor reverse rpc sage lsp llm project
permalink: /posts/reverse-engineering-cursor
featured: false
---

_Check out [cursor-rpc](https://github.com/everestmz/cursor-rpc) on GitHub_

We finally got enterprise Cursor licenses at work - the “blessed” way to use LLMs with our codebase. But I use Helix. I could switch, but there’s a reason I’ve been using it these last few years. The entire model of editing just makes more sense to me than Vim’s style, and there’s no good “Helix mode” plugin for VSCode. In hindsight, it may have been easier to build one of those than what I’m about to do. Too late now!

There has to be a way to just talk to their backend directly…

*

Cursor is a VSCode fork. So somewhere in that app bundle, there must be some JS I can read. I start digging through `Cursor.app/Contents/Resources`. It's mostly standard VSCode stuff, but then - there it is. Grepping for `rpc` in the contents, I come across some mentions of `connectrpc` in `extensionHostProcess.js`. It’s minified and obfuscated, but some of this code looks suspiciously like protobuf definitions. Plus, [ConnectRPC](https://connectrpc.com/docs/introduction) supports streaming - essential for incrementally showing LLM responses.

Now we're getting somewhere.

It looks like the file is meant to be loaded by some AMD module loader: probably by [microsoft/vscode-loader](https://github.com/microsoft/vscode-loader). I spend the next couple hours building a [bootleg Node module loader](https://github.com/everestmz/cursor-rpc/blob/master/cmd/extract/preprocess.js), and a [wrapper](https://github.com/everestmz/cursor-rpc/blob/master/cmd/extract/main.go) to actually load the module and output some protos. The idea is simple: stand up just enough of the module system to get the protobuf type info, and ignore all the things we don’t know how to initialize (like VSCode-specific dependencies). The rest we can just no-op. It's hacky, but it works. 5,000 lines of [protobuf definitions](https://github.com/everestmz/cursor-rpc/blob/master/cursor/aiserver/v1/aiserver.proto), and a few dozen RPC endpoints. We have our structure.

But now I need to figure out how to auth myself with their API...

*

Knowing that most apps store things in `~`, `~/.config`, or `~/Library/Application Support`, I started there. Third time’s the charm: they're storing credentials in a SQLite database. Just sitting there in `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`.

_(From a security perspective, this is probably fine. They could likely lock the permissions down so that only the user can read and write to the db - `600`, like an SSH private key, not `644` like it is now, but the reality is most workstations these days are single user machines)_

Could it be this easy?

```sql
sqlite3 state.vscdb "SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken';"
```

...turns out, yes.

*

The more I dig into these protos, the more interesting patterns I find. They’ve done a lot of interesting context-building work, from file chunking, to bringing in commits, pull requests, and what they call “edit trails”. For now though, I just want to make a successful request. 

Looking at the JS again, it looks like there’s a checksum system too. I have no clue what they’re checksumming, but at least we can port the implementation over.

```javascript
      function S(m) {
        let w = 165;
        for (let E = 0; E < m.length; E++)
          (m[E] = (m[E] ^ w) + (E % 256)), (w = m[E]);
        return m;
      }
     

 function u({
        req: m,
        machineId: w,
        base64Fn: E,
        cursorVersion: L,
        privacyMode: A,
        backupRequestId: F,
      }) {
        try {
          const b = Math.floor(Date.now() / 1e6),
            N = new Uint8Array([
              (b >> 40) & 255,
              (b >> 32) & 255,
              (b >> 24) & 255,
              (b >> 16) & 255,
              (b >> 8) & 255,
              b & 255,
            ]),
            $ = S(N),
            v = E($);
          m.header.set("x-cursor-checksum", `${v}${w}`);
        } catch {}
```

What’s happening here is `S` is basically doing a rolling XOR on a byte array. `u` takes some info like a request and a machine ID. It passes the current date through the rolling XOR function, and then base64 encodes it. It concatenates that with the `machineId`, and then sets the final value as the `x-cursor-checksum` header.

Well I’m not sure what my machineId is, but I can easily replicate that checksum function:

```go
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

```

After running a request... it doesn't actually validate the content? The server just wants to see a checksum header that’s been processed by their algorithm. It doesn't care what's in it. I’m not asking questions - moving on!

*

Building the actual client is almost anticlimactic after all the reverse engineering. A few hundred lines of Go, some header management, and suddenly I'm talking directly to Cursor's backend.

*

It’s interesting what the protos show about Cursor's architecture.

Take their context management. Basic LLM coding tools just grab the current file and maybe a few imports. Cursor's building this rich semantic graph of your codebase. They have different strategies for different types of context - full files, symbols, even something they call "long file scan". It's like they're trying to build a mental model of your code, similar to how a human developer would understand it.

I’ll save the deep dive for later. Right now, I’m just happy I can make inference calls that work.

*

Now that I have a client library, I can get back to what I was doing in the first place: adding Cursor support to [sage](https://github.com/everestmz/sage), my custom LSP-based AI plugin. This basically gives me the best of both worlds - local models for quick tasks, enterprise Cursor access for the heavy lifting.

The integration is surprisingly clean. I just added another [LSP workspace command](https://github.com/everestmz/sage/blob/master/code_actions.go#L159) to sage, so I can pick whether I want Ollama or Cursor to answer my question on the fly. I should probably pull that functionality into a nice library that gives you some common interfaces for all LLM backends (I’ve been meaning to play with Grok anyways - probably a good excuse to do that). _adds to the laundry list of project ideas._

Just like that, any LSP-compatible editor gets to use Cursor’s backend & models. No VSCode needed.

*

Tools should adapt to how we work, not the other way around. One of the perks of being a software engineer is that in situations like this, you can make the tools work for you. Sometimes it involves some spelunking, but now I’ve learned a little more about Cursor, and I get to keep my existing workflow. 

I like this kind of side quest. In and out, no hiccups. Just an interesting story, and back to the main quest: actually writing code.

