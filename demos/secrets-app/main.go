// Command secrets-demo shows Ship Happens variables and secrets in the live TUI.
//
//	# provide the secret via the host environment (never hardcoded):
//	DEPLOY_TOKEN=sk-example-123456 go run ./demos/secrets-app
//
//	go run ./demos/secrets-app                       # missing secret → fails fast
//	DEPLOY_TOKEN=sk-example-123456 go run ./demos/secrets-app --var REGION=us-east
//	DEPLOY_TOKEN=sk-example-123456 go run ./demos/secrets-app --no-tui  # see (masked) logs
//
// The workflow sets a variable REGION and requires a secret DEPLOY_TOKEN. The
// build/test jobs use only the variable; the deploy job additionally consumes
// the secret. Secret values are masked (shown as ***) in all output, are never
// written to the compiled plan, and fail the run fast if absent.
package main

import "github.com/KochC/shipHappens/flow"

func main() {
	wf := flow.New("Deploy Pipeline").
		Var("REGION", "eu-west").
		Var("APP", "widget-svc")

	build := wf.Job("build").
		Run("compile", `echo "building $APP for $REGION" && sleep 0.6`)

	test := wf.Job("test").Needs(build).
		Run("smoke", `echo "smoke test $APP in $REGION" && sleep 0.8`)

	// The deploy job requires DEPLOY_TOKEN from the host environment. It is
	// masked in output, excluded from the plan, and its absence fails fast.
	wf.Job("deploy").Needs(test).
		Secret("DEPLOY_TOKEN").
		Run("push", `echo "deploying $APP to $REGION with token=$DEPLOY_TOKEN" && sleep 0.5`).
		Run("verify", `echo "verifying deployment (token still masked: $DEPLOY_TOKEN)"`)

	flow.RunWithTUI(wf)
}
