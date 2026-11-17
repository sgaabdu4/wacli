package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWithStdin(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("WACLI_STORE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "stdin.json")
	if werr := os.WriteFile(path, []byte(stdin), 0o600); werr != nil {
		t.Fatal(werr)
	}
	f, oerr := os.Open(path)
	if oerr != nil {
		t.Fatal(oerr)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = orig; _ = f.Close() })
	stderr = captureRootStderr(t, func() {
		stdout = captureRootStdout(t, func() { err = execute(args) })
	})
	return stdout, stderr, err
}

func TestValuesStdinRefusesUnknownFlagName(t *testing.T) {
	_, stderr, err := runWithStdin(t, `{"flags":{"nope":"x"}}`, "--values-stdin", "messages", "show")
	if err == nil || !strings.Contains(stderr+err.Error(), "nope") {
		t.Fatalf("want unknown flag error naming nope, got err=%v stderr=%q", err, stderr)
	}
}

func TestValuesStdinRefusesStoreAccountAndItself(t *testing.T) {
	for _, name := range []string{"store", "account", "values-stdin"} {
		_, _, err := runWithStdin(t, `{"flags":{"`+name+`":"x"}}`, "--values-stdin", "messages", "show")
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("flag %s: want refusal, got %v", name, err)
		}
	}
}

func TestValuesStdinRefusesEmptyAndInvalidInput(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"blank":        "  \n",
		"invalid":      "{nope",
		"unknown key":  `{"flags":{},"extra":1}`,
		"two objects":  `{} {}`,
		"flag as null": `{"flags":{"chat":null}}`,
		"flag object":  `{"flags":{"chat":{}}}`,
	}
	for name, input := range cases {
		_, _, err := runWithStdin(t, input, "--values-stdin", "messages", "show")
		if err == nil || !strings.Contains(err.Error(), "values-stdin") {
			t.Errorf("%s: want values-stdin error, got %v", name, err)
		}
	}
}

func TestValuesStdinKeepsDashValuesLiteral(t *testing.T) {
	_, stderr, err := runWithStdin(t, `{"flags":{"chat":"--to=y","id":"-x"}}`, "--values-stdin", "messages", "show")
	if err == nil || strings.Contains(stderr, "unknown") || strings.Contains(stderr, "required") {
		t.Fatalf("flag values were parsed as options: err=%v stderr=%q", err, stderr)
	}
}

func TestValuesStdinKeepsDashPositionalLiteral(t *testing.T) {
	stdout, stderr, err := runWithStdin(t, `{"args":["-x"]}`, "--values-stdin", "--json", "contacts", "search")
	if err != nil {
		t.Fatalf("positional -x refused: %v %q", err, stderr)
	}
	if !strings.Contains(stdout, `"success":true`) {
		t.Fatalf("unexpected output %q", stdout)
	}
}

func TestValuesStdinPositionalAfterExistingDoubleDash(t *testing.T) {
	stdout, stderr, err := runWithStdin(t, `{"args":["--later"]}`, "--values-stdin", "--json", "contacts", "search", "--", "-x")
	if err == nil || !strings.Contains(err.Error()+stderr, "accepts 1 arg") {
		t.Fatalf("want two positionals refused by arg count, got err=%v out=%q", err, stdout)
	}
}

func TestValuesStdinHelpIsNotAFlagName(t *testing.T) {
	_, _, err := runWithStdin(t, `{"flags":{"help":true}}`, "--values-stdin", "messages", "show")
	if err == nil {
		t.Fatal("help is not a defined flag name and must be refused")
	}
}

func TestValuesStdinLeavesNoStdinForPrompts(t *testing.T) {
	_, _, _ = runWithStdin(t, `{}`, "--values-stdin", "--json", "contacts", "search", "x")
	buf := make([]byte, 1)
	n, _ := os.Stdin.Read(buf)
	if n != 0 || isInteractive() {
		t.Fatalf("stdin must be exhausted and non-interactive after the values are read")
	}
}
