package dst_test

import (
	"bytes"
	"strings"
	"testing"

	mcpdst "github.com/donaldgifford/mcp-go-gen/internal/dst"
)

const simpleMain = `package main

import (
	"context"
	"log"
)

func main() {
	ctx := context.Background()
	app := newApp()
	cfg := loadConfig()
	_ = ctx
	_ = app
	_ = cfg

	// mcpgen:hook

	log.Println("app started")
	select {}
}

func newApp() int { return 0 }
func loadConfig() int { return 0 }
`

const simpleMainNoHook = `package main

import "log"

func main() {
	log.Println("hi")
}
`

func TestEdit_InsertsRegisterCall(t *testing.T) {
	t.Parallel()

	res, err := mcpdst.Edit([]byte(simpleMain), "example.com/svc/internal/mcpserver", "mcpserver")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if !res.Changed {
		t.Fatal("Changed = false on first edit")
	}
	got := string(res.Source)
	if !strings.Contains(got, `mcpserver.Register`) {
		t.Errorf("output missing Register call:\n%s", got)
	}
	if !strings.Contains(got, `"example.com/svc/internal/mcpserver"`) {
		t.Errorf("output missing import:\n%s", got)
	}
	if !strings.Contains(got, "// mcpgen:hook") {
		t.Errorf("hook comment dropped from output:\n%s", got)
	}
}

func TestEdit_IsIdempotent(t *testing.T) {
	t.Parallel()

	pass1, err := mcpdst.Edit([]byte(simpleMain), "example.com/svc/internal/mcpserver", "mcpserver")
	if err != nil {
		t.Fatal(err)
	}
	pass2, err := mcpdst.Edit(pass1.Source, "example.com/svc/internal/mcpserver", "mcpserver")
	if err != nil {
		t.Fatal(err)
	}
	if pass2.Changed {
		t.Error("second pass reported Changed=true; edit is not idempotent")
	}
	if !bytes.Equal(pass1.Source, pass2.Source) {
		t.Errorf("second pass output differs:\n--- pass1 ---\n%s\n--- pass2 ---\n%s",
			pass1.Source, pass2.Source)
	}
}

func TestEdit_RejectsMissingHook(t *testing.T) {
	t.Parallel()

	_, err := mcpdst.Edit([]byte(simpleMainNoHook), "example.com/svc/internal/mcpserver", "mcpserver")
	if err == nil {
		t.Fatal("expected error for missing hook")
	}
	if !strings.Contains(err.Error(), "mcpgen:hook") {
		t.Errorf("err = %v, want mentions mcpgen:hook", err)
	}
}

func TestEdit_RejectsMissingMain(t *testing.T) {
	t.Parallel()

	src := `package main

import "log"

func runner() {
	// mcpgen:hook
	log.Println("no main here")
}
`
	_, err := mcpdst.Edit([]byte(src), "example.com/svc/internal/mcpserver", "mcpserver")
	if err == nil {
		t.Fatal("expected error for missing main func")
	}
	if !strings.Contains(err.Error(), "no func main") {
		t.Errorf("err = %v, want mentions 'no func main'", err)
	}
}

func TestEdit_PreservesExistingComments(t *testing.T) {
	t.Parallel()

	src := `package main

import "log"

// userComment documents something important.
func main() {
	log.Println("starting")

	// mcpgen:hook

	log.Println("done")
}
`
	res, err := mcpdst.Edit([]byte(src), "example.com/svc/internal/mcpserver", "mcpserver")
	if err != nil {
		t.Fatal(err)
	}
	out := string(res.Source)
	if !strings.Contains(out, "userComment documents something important") {
		t.Errorf("user comment lost:\n%s", out)
	}
	if !strings.Contains(out, `log.Println("starting")`) {
		t.Errorf("pre-hook statement lost:\n%s", out)
	}
	if !strings.Contains(out, `log.Println("done")`) {
		t.Errorf("post-hook statement lost:\n%s", out)
	}
}
