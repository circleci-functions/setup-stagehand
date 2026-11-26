package main

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestBaseURL(t *testing.T) {
	t.Setenv("DEPLOYMENT_URL", "")
	t.Setenv("VERCEL_DEPLOYMENT_URL", "")
	assert.Check(t, cmp.Equal(baseURL(""), ""))
	assert.Check(t, cmp.Equal(baseURL("my-app.example.com/"), "https://my-app.example.com"))
	assert.Check(t, cmp.Equal(baseURL("localhost:3000"), "http://localhost:3000"))
	assert.Check(t, cmp.Equal(baseURL("127.0.0.1:8080/app/"), "http://127.0.0.1:8080/app"))
	assert.Check(t, cmp.Equal(baseURL("http://web:3000"), "http://web:3000"))

	t.Setenv("VERCEL_DEPLOYMENT_URL", "preview.vercel.app")
	assert.Check(t, cmp.Equal(baseURL(""), "https://preview.vercel.app"))
	t.Setenv("DEPLOYMENT_URL", "https://deployed.example.com")
	assert.Check(t, cmp.Equal(baseURL(""), "https://deployed.example.com"))
	assert.Check(t, cmp.Equal(baseURL("https://flag.example.com"), "https://flag.example.com"))
}

func TestAnyAPIKey(t *testing.T) {
	assert.Check(t, anyAPIKey([]string{"OPENROUTER_API_KEY=sk-or"}))
	assert.Check(t, anyAPIKey([]string{"MODEL_API_KEY=x"}))
	assert.Check(t, !anyAPIKey([]string{"BROWSERBASE_API_KEY=bb"}))
	assert.Check(t, !anyAPIKey([]string{"ANTHROPIC_API_KEY=", "PATH=/bin"}))
}

func TestNodeRunsStagehand4(t *testing.T) {
	assert.Check(t, nodeRunsStagehand4("22.18.0"))
	assert.Check(t, nodeRunsStagehand4("24.1.0"))
	assert.Check(t, !nodeRunsStagehand4("22.17.9"))
	assert.Check(t, !nodeRunsStagehand4("v22"))
}
