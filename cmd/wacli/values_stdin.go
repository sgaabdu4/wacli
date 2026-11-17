package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const valuesStdinFlag = "values-stdin"

var valuesStdinRefused = map[string]bool{
	valuesStdinFlag: true,
	"store":         true,
	"account":       true,
}

type stdinValues struct {
	Flags map[string]json.RawMessage `json:"flags"`
	Args  []string                   `json:"args"`
}

func valuesStdinRequested(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--"+valuesStdinFlag {
			return true
		}
		if v, ok := strings.CutPrefix(a, "--"+valuesStdinFlag+"="); ok {
			on, err := strconv.ParseBool(v)
			return err == nil && on
		}
	}
	return false
}

func readStdinValues(r io.Reader) (*stdinValues, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("--%s: read stdin: %w", valuesStdinFlag, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("--%s: stdin is empty; expected one JSON object", valuesStdinFlag)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var v stdinValues
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("--%s: invalid JSON on stdin: %w", valuesStdinFlag, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("--%s: stdin must hold exactly one JSON object", valuesStdinFlag)
	}
	return &v, nil
}

func applyStdinValues(root *cobra.Command, words []string, v *stdinValues) ([]string, error) {
	cmd, _, err := root.Find(words)
	if err != nil {
		return nil, err
	}
	cmd.InheritedFlags()
	names := make([]string, 0, len(v.Flags))
	for name := range v.Flags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if valuesStdinRefused[name] {
			return nil, fmt.Errorf("--%s: flag %q cannot be set from stdin", valuesStdinFlag, name)
		}
		f := cmd.Flags().Lookup(name)
		if f == nil {
			return nil, fmt.Errorf("--%s: %q is not a flag of %q", valuesStdinFlag, name, cmd.CommandPath())
		}
		if err := setStdinFlag(cmd.Flags(), f, v.Flags[name]); err != nil {
			return nil, fmt.Errorf("--%s: flag %q: %w", valuesStdinFlag, name, err)
		}
	}
	out := append([]string{}, words...)
	if len(v.Args) > 0 {
		if !containsDoubleDash(words) {
			out = append(out, "--")
		}
		out = append(out, v.Args...)
	}
	return out, nil
}

func containsDoubleDash(words []string) bool {
	for _, w := range words {
		if w == "--" {
			return true
		}
	}
	return false
}

func setStdinFlag(fs *pflag.FlagSet, f *pflag.Flag, raw json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return err
	}
	if list, ok := value.([]any); ok {
		items := make([]string, 0, len(list))
		for _, item := range list {
			s, err := scalarString(item)
			if err != nil {
				return err
			}
			items = append(items, s)
		}
		sv, ok := f.Value.(pflag.SliceValue)
		if !ok {
			return fmt.Errorf("is not a repeatable flag; pass one value")
		}
		if err := sv.Replace(items); err != nil {
			return err
		}
		f.Changed = true
		return nil
	}
	s, err := scalarString(value)
	if err != nil {
		return err
	}
	if _, ok := f.Value.(pflag.SliceValue); ok {
		return fmt.Errorf("is a repeatable flag; pass an array")
	}
	return fs.Set(f.Name, s)
}

func scalarString(value any) (string, error) {
	switch x := value.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case bool:
		return strconv.FormatBool(x), nil
	default:
		return "", fmt.Errorf("value must be a string, number, bool or array of those")
	}
}

func detachStdin() error {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	os.Stdin = devNull
	return nil
}

func loadStdinValues(root *cobra.Command, args []string) ([]string, error) {
	v, err := readStdinValues(os.Stdin)
	if err != nil {
		return nil, err
	}
	if err := detachStdin(); err != nil {
		return nil, err
	}
	return applyStdinValues(root, args, v)
}
