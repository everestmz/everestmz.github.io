---
layout: post
title:  "Sage: Hijacking LSP to build a universal LLM plugin"
subtitle: How I used the Language Server Protocol to build an LLM plugin that works with any editor 
date:   2024-12-01
tags: sage lsp llm project
permalink: /posts/introducing-sage
featured: true
---

My editor crashed again. Somewhere in our 15-million-line codebase was the function I needed, but [Pyright](https://github.com/microsoft/pyright) had other ideas. Third crash that day. Same story: workspace symbol search bringing my dev environment to its knees.

At the time, I’d been bouncing between [Helix](https://helix-editor.com/) and [Cursor](https://www.cursor.com/) for months. Cursor's AI features are great - especially being able to `@mention` symbols to include them in context. But Helix's keybinds were muscle memory at this point. If only it had plugin support…

I was knee-deep in pyright’s codebase when it hit me. I'd been staring at the solution every day without realizing it: the [Language Server Protocol](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/). The same interface that let Pyright talk to my editor could let me inject AI features anywhere. Even better, I could fix that symbol search problem at the same time.

Two problems, one solution. Here’s what I did.

## Solving Symbol Search

Let's start with the simpler problem: searching 15 million lines of code. Pyright could handle symbols in a single file fine - it just choked when trying to search everything at once.

Classic "works for 1, explodes for n" situation.

My first thought was "how hard could it be?" Famous last words. But seriously - we're just searching text. Plenty of tools can grep through codebases this size in seconds. The difference with tools like GitHub’s symbol search is they're doing it ahead of time, not trying to parse and analyze code on the fly.

I realized: I didn't need real-time either. I could pre-compute everything.

## A Database Detour

"Use SQLite" has become a bit of a meme recently, but hear me out. I needed a DB that could store a few million symbols, run basic queries, and support features like semantic search down the line (shoutout [sqlite-vec](https://github.com/asg017/sqlite-vec)). Most importantly, I didn't want to think about infrastructure - no extra services to stand up, no careful tuning, just symbols go in, symbols come out.

The schema was pretty simple - I just needed to store all of the codebase’s symbols, as well as the files they came from (so I wasn’t re-indexing things that hadn’t changed):

```sql
CREATE TABLE IF NOT EXISTS symbol (
  id INTEGER PRIMARY KEY,
  kind REAL NOT NULL, -- stores the symbol type: function, class, etc
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  start_line INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_line INTEGER NOT NULL,
  end_col INTEGER NOT NULL,
  file_id INTEGER NOT NULL,
  FOREIGN KEY(file_id) REFERENCES file(id)
);
```

## The First Run

The indexing code should have been straightforward. Bring up an instance of pyright, walk the repo extracting symbols file-by-file, and store them in the SQLite DB. Reality is, I needed a couple of hacks (turns out pyright grinds to a halt after indexing 200k symbols, even if you close each file after processing it - probably why global search doesn't work). But before long, I was ready to churn through the whole repo.

I'm going to be honest - the first indexing run was painful. Basically just me checking CPU usage every couple hours as my indexer churned through files all night. But the next morning, I had a 3GB SQLite database full of every function, class, and variable in the codebase.

_Sidebar: a whole night is a **long time** to wait to index a codebase. I recently [built a tool](https://github.com/everestmz/llmcat) that allowed me to get this indexing time down to ~10 minutes, but I’ll save that for another post._

The moment of truth came when I tried my first query:

```sql
SELECT * FROM symbols WHERE name LIKE 'UserAuth%' LIMIT 100;
```

Under 50ms. No crashes, no swapping, no angry "LSP server exited" messages. Just instant results from a 3GB database of symbols. A far cry from the never-ending searches I was used to. Sometimes the boring solutions really are the best ones.

## LSP Sleight of Hand

Now for the fun part. How do you add features to an editor that doesn't support plugins? You pretend to be something it already trusts. Every modern editor speaks LSP - it's how they interact with language servers like Pyright. The protocol is simple: the editor sends requests like "what's the definition of this symbol?" or "search for symbols matching this text", and the server responds.

The best part is: nothing says you have to be a real language server. You can be a proxy that passes most requests through to the real language server, intercepts the interesting ones, and adds entirely new capabilities.

Think of it like a reverse proxy for your editor. When your editor starts up, it sends an "initialize" request to discover what the language server can do. This is our chance to hook in: 

```go
// When the editor tries to initialize us...
case protocol.MethodInitialize:
  // First, pass the request to the real language server
  childServer, err := startLsp(config)

  // Get its capabilities
  capabilities := childServer.InitResult.Capabilities

  // Add our own capabilities
  capabilities.WorkspaceSymbolProvider = true  // We handle symbol search now

  // Register our custom AI commands
  capabilities.ExecuteCommandProvider.Commands = append(
      capabilities.ExecuteCommandProvider.Commands,
      // ...our custom commands, like AI completions
  )

  // The editor now thinks we're a very capable language server
  return capabilities
```
The best part about this approach is that it's universal. Helix, VSCode, Vim - if it speaks LSP, it'll work with Sage. No plugins required.

# Making AI Features Universal

This is where things get really interesting. With our LSP proxy approach, we can bring AI features to any editor.

There's this handy LSP feature called `workspace/executeCommand`. It's meant for things like "format code" or "organize imports", but we can use it for something more fun: talking to LLMs:

![Demoing sage LLM completions](/assets/images/sage-demo.gif)

Adding a new command to sage is easy - just implement this interface:

```go
type CommandDefinition struct {
  Title          string
  Identifier     string
  ShowCodeAction bool
  BuildArgs      func(params *protocol.CodeActionParams) (args []any, err error)
  Execute        func(params *protocol.ExecuteCommandParams, client LspClient, clientInfo *LanguageServerClientInfo) (*protocol.ApplyWorkspaceEditParams, error)
}
```

But I wanted to go further. Cursor's `@mention` feature is great - you can reference a function and include its implementation in the context window. I wanted something similar, but with a little more flexibility. Sometimes you want to pull in specific line ranges to show how a function is used, or include an entire configuration file for context. I wrote a small but powerful context definition DSL:

```text
# Pull in a whole file
config.go

# Grab specific symbols
auth.go UserAuthenticate validateToken

# Include line ranges
database.go 15:45
```

Each line tells Sage exactly what context to include, and our SQLite index makes retrieving any of these lightning fast.

Here's what happens when you trigger an AI completion in Sage. First, the editor sends a `workspace/executeCommand`, and our proxy intercepts it. Then, Sage:

- Grabs the current file context 
- Uses the highlighted part of the file as the prompt
- Reads your context configuration
- Builds the context window by:
  - Pulling whole files directly
  - Loading in any referenced symbols (using our blazing fast™ SQLite index)
  - Grabs specific line ranges when needed
  - Combines it with the original prompt
- Sends it to a local model via Ollama

The results stream back through our proxy to the editor through a series of `workspace/applyEdit` LSP calls.

LSP lets us inject features anywhere, SQLite makes symbol lookup instant, and Ollama handles AI locally. No editor plugins, no remote API calls, no performance issues - just the features I wanted in any editor I choose.

## The Result

Now, I have the best of both worlds: AI integration and symbol search without context switching between editors, or crashing pyright.

But the real discovery wasn't just solving these specific problems - by treating LSP as a universal plugin interface rather than just a protocol for language features, we can extend any editor without writing editor-specific code.

It's a bit like figuring out that your house's electrical system can do more than just carry power - reminds me of how those powerline wifi extenders blew my mind when I was in high school. LSP wasn't designed as a universal plugin system, but with a bit of creative thinking, that's exactly what it becomes.

## What's Next?
I’m fascinated by the idea of LSP as a universal interface for extending how we interact with code. Think about what git hooks did for version control - LSP should be the same thing for editing, a true universal plugin interface. Somehow, our tools haven't caught up with that yet.

Imagine being able to write a quick script that then gets bound into your editor immediately:

```lua
on("textDocument/didChange", function(change)
    -- Analyze the changes
    -- Update machine learning models
    -- Trigger automations
end)
```

Or having your editor automatically build context based on what you're working on. Instead of manually specifying what symbols to include, your editor could:

- Extract functions from the context around your cursor
- Track which functions you commonly look at together
- Include what you edited most recently
- Build context from your last test run, terminal command, or git commit

I’ve been pretty obsessed with that last part recently - using the tools we already have available to make context windows as good as possible. The goal is really to make AI features feel as natural as syntax highlighting - another part of your editor that just works. More on that in another post…

Right now, I’m just excited by the idea of non-traditional tools using LSP. Imagine a proxy that makes _every_ LSP command instant, for an entire organization of developers, by offloading indexing to a beefy cloud cluster? Or what about streaming live RLHF data as developers edit code in realtime? Sometimes misusing tools produces the most interesting outcomes...
