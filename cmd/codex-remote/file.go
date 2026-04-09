package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strconv"

	"codex-runner/internal/codexremote/client"
	"codex-runner/internal/shared/jsonutil"
)

func fileCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: codex-remote file <write|read> [flags]")
		os.Exit(2)
	}
	switch args[0] {
	case "write":
		fileWrite(args[1:])
	case "read":
		fileRead(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown file subcommand: %s\n", args[0])
		os.Exit(2)
	}
}

func fileWrite(args []string) {
	fs := flag.NewFlagSet("file write", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machine := fs.String("machine", "", "machine name")
	dst := fs.String("dst", "", "remote destination path (absolute)")
	content := fs.String("content", "", "inline content string")
	src := fs.String("src", "", "local source file path")
	modeStr := fs.String("mode", "0644", "file permission (octal)")
	mkdirP := fs.Bool("mkdir", false, "create parent directories")
	_ = fs.Parse(args)

	if *machine == "" {
		fmt.Fprintln(os.Stderr, "error: --machine is required")
		os.Exit(2)
	}
	if *dst == "" {
		fmt.Fprintln(os.Stderr, "error: --dst is required")
		os.Exit(2)
	}
	if *content == "" && *src == "" {
		fmt.Fprintln(os.Stderr, "error: --content or --src is required")
		os.Exit(2)
	}
	if *content != "" && *src != "" {
		fmt.Fprintln(os.Stderr, "error: --content and --src are mutually exclusive")
		os.Exit(2)
	}

	var data []byte
	if *src != "" {
		var err error
		data, err = os.ReadFile(*src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading source file: %v\n", err)
			os.Exit(1)
		}
	} else {
		data = []byte(*content)
	}

	mode64, err := strconv.ParseUint(*modeStr, 8, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing mode: %v\n", err)
		os.Exit(2)
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(2)
	}
	m, ok := cfg.FindMachine(*machine)
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown machine:", *machine)
		os.Exit(2)
	}
	cl, closer, err := connectClient(*m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}

	ctx := context.Background()
	resp, err := cl.FileWrite(ctx, client.FileWriteRequest{
		Path:    *dst,
		Content: base64.StdEncoding.EncodeToString(data),
		Mode:    int(mode64),
		MkdirP:  *mkdirP,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	_ = jsonutil.WriteJSON(os.Stdout, resp)
}

func fileRead(args []string) {
	fs := flag.NewFlagSet("file read", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machine := fs.String("machine", "", "machine name")
	path := fs.String("path", "", "remote file path (absolute)")
	dst := fs.String("dst", "", "local destination file (optional, otherwise prints to stdout)")
	_ = fs.Parse(args)

	if *machine == "" {
		fmt.Fprintln(os.Stderr, "error: --machine is required")
		os.Exit(2)
	}
	if *path == "" {
		fmt.Fprintln(os.Stderr, "error: --path is required")
		os.Exit(2)
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(2)
	}
	m, ok := cfg.FindMachine(*machine)
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown machine:", *machine)
		os.Exit(2)
	}
	cl, closer, err := connectClient(*m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}

	ctx := context.Background()
	resp, err := cl.FileRead(ctx, *path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dst != "" {
		data, err := base64.StdEncoding.DecodeString(resp.Content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error decoding content: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*dst, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote %d bytes to %s\n", len(data), *dst)
	} else {
		_ = jsonutil.WriteJSON(os.Stdout, resp)
	}
}
