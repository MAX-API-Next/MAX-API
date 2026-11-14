// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Implements the published QuantumNous Task Plugin API v1 using MoeJS.
// No upstream host implementation is copied; SDK semantics are referenced at
// new-api e00ec8819 and new-api-plugins 2a8288645 (docs/plugin-api/v1.d.ts).
package taskplugin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/engine"
	"github.com/google/uuid"
)

var (
	ErrPluginCompile = errors.New("task plugin compilation or metadata validation failed")
	ErrPluginHook    = errors.New("task plugin hook failed")
	ErrPluginLimit   = errors.New("task plugin execution limit exceeded")
	ErrPluginSDK     = errors.New("task plugin SDK capability unavailable")
)

// RuntimeOptions is host-owned, never supplied by plugin metadata. Zero uses
// bounded defaults. Network/filesystem/process/DB access are not installed.
type RuntimeOptions struct {
	Timeout     time.Duration
	MemoryLimit int64
	Now         func() time.Time
}

// Program is immutable. Calls use distinct runtimes: neither mutable globals,
// module state nor credentials can leak between requests or concurrent users.
type Program struct {
	module  *moejs.Module
	options RuntimeOptions
	slots   chan struct{}
}

func CompileJavaScript(source string, options RuntimeOptions) (*Program, error) {
	if len(source) == 0 || len(source) > MaxPackageBytes || !utf8.ValidString(source) {
		return nil, ErrPluginCompile
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Second
	}
	if options.MemoryLimit == 0 {
		options.MemoryLimit = 32 << 20
	}
	if options.Timeout < time.Millisecond || options.Timeout > 5*time.Second || options.MemoryLimit < 1<<20 || options.MemoryLimit > 128<<20 {
		return nil, ErrPluginCompile
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	module, err := moejs.Compile("task-plugin.js", source)
	if err != nil {
		return nil, ErrPluginCompile
	}
	p := &Program{module: module, options: options, slots: make(chan struct{}, 4)}
	// Includes top-level execution and metadata getters in the same limits.
	if _, err = p.Export(context.Background(), "meta"); err != nil {
		return nil, err
	}
	return p, nil
}

func runtimeFailure(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, moejs.ErrMemoryLimit) || errors.Is(err, moejs.ErrTooLarge) || errors.Is(err, moejs.ErrTooDeep) {
		return ErrPluginLimit
	}
	// JavaScript may throw channel credentials or arbitrary source text.
	return ErrPluginHook
}

func (p *Program) run(ctx context.Context, action func(*moejs.Runtime) (moejs.Value, error)) (json.RawMessage, error) {
	if ctx == nil || p == nil {
		return nil, ErrPluginHook
	}
	ctx, cancel := context.WithTimeout(ctx, p.options.Timeout)
	defer cancel()
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	case <-ctx.Done():
		return nil, runtimeFailure(ctx.Err())
	}
	rt := moejs.NewRuntime(moejs.Options{DisableDynamicCode: true, TimeZone: time.UTC, MemoryLimit: p.options.MemoryLimit})
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { rt.Interrupt(context.Cause(ctx)); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
	}()
	if err := p.installUtils(rt); err != nil {
		return nil, runtimeFailure(err)
	}
	if err := rt.Load(p.module); err != nil {
		return nil, runtimeFailure(err)
	}
	v, err := action(rt)
	if err != nil {
		return nil, runtimeFailure(err)
	}
	if v.IsObject() && v.AsObject().Class() == engine.ClassPromise {
		return nil, ErrPluginHook
	}
	if v.IsUndefined() {
		return json.RawMessage("null"), nil
	}
	var raw json.RawMessage
	if err := rt.UnmarshalWith(v, &raw, moejs.DecodeOptions{MaxBytes: MaxPackageBytes, MaxNodes: 32768}); err != nil {
		return nil, runtimeFailure(err)
	}
	if ctx.Err() != nil {
		return nil, runtimeFailure(ctx.Err())
	}
	return raw, nil
}

func (p *Program) Export(ctx context.Context, name string) (json.RawMessage, error) {
	return p.run(ctx, func(rt *moejs.Runtime) (moejs.Value, error) {
		v, ok := rt.Export(name)
		if !ok {
			return moejs.Undefined(), ErrPluginHook
		}
		return v, nil
	})
}

func (p *Program) hasFunctions(ctx context.Context, paths []string) bool {
	_, err := p.run(ctx, func(rt *moejs.Runtime) (moejs.Value, error) {
		for _, path := range paths {
			parts := strings.Split(path, ".")
			v, ok := rt.Export(parts[0])
			if !ok {
				return moejs.Undefined(), ErrPluginCompile
			}
			for _, member := range parts[1:] {
				var err error
				v, err = rt.Get(v, member)
				if err != nil {
					return moejs.Undefined(), err
				}
			}
			if !engine.IsCallable(v) {
				return moejs.Undefined(), ErrPluginCompile
			}
		}
		return moejs.Bool(true), nil
	})
	return err == nil
}

// Call accepts JSON-compatible host values only. Results retain JavaScript's
// JSON key order, including request bodies that participate in signatures.
func (p *Program) Call(ctx context.Context, path string, args ...any) (json.RawMessage, error) {
	parts := strings.Split(path, ".")
	if len(parts) > 3 || len(parts) == 0 {
		return nil, ErrPluginHook
	}
	for _, part := range parts {
		if !identifierPattern.MatchString(part) {
			return nil, ErrPluginHook
		}
	}
	hook, err := p.module.Hook(parts[0], parts[1:]...)
	if err != nil {
		return nil, ErrPluginHook
	}
	return p.run(ctx, func(rt *moejs.Runtime) (moejs.Value, error) {
		values := make([]moejs.Value, len(args))
		for i, arg := range args {
			raw, err := common.Marshal(arg)
			if err != nil || len(raw) > MaxPackageBytes {
				return moejs.Undefined(), ErrPluginHook
			}
			values[i], err = rt.ParseJSON(raw)
			if err != nil {
				return moejs.Undefined(), err
			}
		}
		return rt.Call(hook, values...)
	})
}

func sdkString(r *moejs.Realm, args []moejs.Value, i int) (string, error) {
	v := moejs.Arg(args, i)
	if !v.IsString() {
		return "", ErrPluginSDK
	}
	s, err := r.ToString(v)
	if err != nil {
		return "", ErrPluginSDK
	}
	return s.GoString(), nil
}

func (p *Program) installUtils(rt *moejs.Runtime) error {
	stringFn := func(fn func(string) (string, error)) moejs.NativeFunc {
		return func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			if len(args) != 1 {
				return moejs.Undefined(), ErrPluginSDK
			}
			s, err := sdkString(r, args, 0)
			if err != nil {
				return moejs.Undefined(), err
			}
			out, err := fn(s)
			return moejs.String(out), err
		}
	}
	unsupported := moejs.NativeFunc(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
		return moejs.Undefined(), ErrPluginSDK
	})
	utils := map[string]any{
		"unixNow": moejs.NativeFunc(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
			return moejs.Int(p.options.Now().Unix()), nil
		}),
		"uuid": moejs.NativeFunc(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
			return moejs.String(uuid.NewString()), nil
		}),
		"hasCapability": moejs.NativeFunc(func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			s, err := sdkString(r, args, 0)
			return moejs.Bool(s == "json-clone@1" || s == "duration-auto@1"), err
		}),
		"base64":    stringFn(func(s string) (string, error) { return base64.StdEncoding.EncodeToString([]byte(s)), nil }),
		"base64URL": stringFn(func(s string) (string, error) { return base64.RawURLEncoding.EncodeToString([]byte(s)), nil }),
		"base64URLDecode": stringFn(func(s string) (string, error) {
			b, err := base64.RawURLEncoding.DecodeString(s)
			if err != nil || !utf8.Valid(b) {
				return "", ErrPluginSDK
			}
			return string(b), nil
		}),
		"sha256": stringFn(func(s string) (string, error) {
			sum := sha256.Sum256([]byte(s))
			return hex.EncodeToString(sum[:]), nil
		}),
		"hmacSHA256": moejs.NativeFunc(func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			message, err := sdkString(r, args, 0)
			if err != nil {
				return moejs.Undefined(), err
			}
			key, err := sdkString(r, args, 1)
			if err != nil {
				return moejs.Undefined(), err
			}
			// Other encodings/byte keys are not silently accepted as hex.
			if len(args) > 2 {
				return moejs.Undefined(), ErrPluginSDK
			}
			h := hmac.New(sha256.New, []byte(key))
			_, _ = h.Write([]byte(message))
			return moejs.String(hex.EncodeToString(h.Sum(nil))), nil
		}),
		"jwtSignHS256": moejs.NativeFunc(func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			key, err := sdkString(r, args, 1)
			if err != nil {
				return moejs.Undefined(), err
			}
			var claims json.RawMessage
			if err := rt.UnmarshalWith(moejs.Arg(args, 0), &claims, moejs.DecodeOptions{MaxBytes: 64 << 10, MaxNodes: 1024}); err != nil {
				return moejs.Undefined(), ErrPluginSDK
			}
			if len(claims) == 0 || claims[0] != '{' {
				return moejs.Undefined(), ErrPluginSDK
			}
			unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
			h := hmac.New(sha256.New, []byte(key))
			_, _ = h.Write([]byte(unsigned))
			return moejs.String(unsigned + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))), nil
		}),
		"fetch": unsupported, "volcSignV4": unsupported, "signRS256": unsupported,
		"json": map[string]any{"clone": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			var raw json.RawMessage
			if err := rt.UnmarshalWith(moejs.Arg(args, 0), &raw, moejs.DecodeOptions{MaxBytes: MaxPackageBytes, MaxNodes: 32768}); err != nil {
				return moejs.Undefined(), ErrPluginSDK
			}
			var tree any
			if common.Unmarshal(raw, &tree) != nil || !cloneDepthAllowed(tree, 0) {
				return moejs.Undefined(), ErrPluginSDK
			}
			return rt.ParseJSON(raw)
		})},
	}
	return rt.SetGlobal("utils", utils)
}

func cloneDepthAllowed(value any, depth int) bool {
	if depth > 32 {
		return false
	}
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			if !cloneDepthAllowed(child, depth+1) {
				return false
			}
		}
	case []any:
		for _, child := range value {
			if !cloneDepthAllowed(child, depth+1) {
				return false
			}
		}
	}
	return true
}
