package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	ase "github.com/Jofdiazdi/aseprite-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		log.New(os.Stderr, "aseprite-mcp: ", 0).Print(err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		return fmt.Errorf("usage: aseprite-mcp serve --mode live|batch --workspace PATH [--aseprite PATH]")
	}
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	mode := f.String("mode", "live", "live or batch")
	workspace := f.String("workspace", ".", "existing artwork workspace")
	executable := f.String("aseprite", "aseprite", "Aseprite executable (batch mode)")
	timeout := f.Duration("timeout", 30*time.Second, "deadline per Aseprite operation")
	if err := f.Parse(os.Args[2:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if err := ase.ValidateMode(*mode); err != nil {
		return err
	}
	w, err := ase.OpenWorkspace(*workspace)
	if err != nil {
		return err
	}
	defer w.Close()
	var backend ase.Backend
	if *mode == "batch" {
		backend, err = ase.NewBatch(*executable, *timeout)
	} else {
		var live *ase.Live
		live, err = ase.NewLive(*timeout)
		backend = live
		if err == nil {
			fmt.Fprintln(os.Stderr, "In Aseprite choose File > Scripts > Agent MCP: Connect / Status; pairing file:", live.PairingFile)
		}
	}
	if err != nil {
		return err
	}
	defer backend.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return ase.NewServer(ase.NewService(backend, w)).Run(ctx, &mcp.StdioTransport{})
}
