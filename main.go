// Command setup-stagehand puts a Chrome that Stagehand can drive on a CircleCI
// job, and runs a project's Stagehand scripts or tests against the site under
// test, locally or on Browserbase.
//
// Supported platforms: linux, darwin, windows.
package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"
)

type cli struct {
	Run     runCmd     `cmd:"" default:"withargs" help:"Install Chrome if needed, then run the project's Stagehand scripts or tests. Runs when no command is given."`
	Install installCmd `cmd:"" help:"Install a Chrome that Stagehand can drive, caching it across jobs."`
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-stagehand"),
		kong.Description("Install Chrome for Stagehand, and run Stagehand scripts or tests locally or on Browserbase."),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run()
}
