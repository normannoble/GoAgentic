package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/normannoble/GoAgentic/internal/command"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(command.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
