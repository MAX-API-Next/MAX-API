/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package billingexpr

import (
	"math"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func taskQuoteInput(expression string) TaskQuoteInput {
	return TaskQuoteInput{Profile: TaskExpressionProfile, Expression: expression,
		Usage: map[string]any{"units": float64(1)}, QuotaPerUnit: 1000, GroupRatio: 1,
		PricingAt: time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC)}
}

func TestTaskUsageReferencesBeforeOptimization(t *testing.T) {
	keys, err := TaskUsageKeys(`false ? u("hidden") : u("units")`)
	if err != nil || !reflect.DeepEqual(keys, []string{"hidden", "units"}) {
		t.Fatal(keys, err)
	}
	keys[0] = "mutated"
	again, err := TaskUsageKeys(`false ? u("hidden") : u("units")`)
	if err != nil || again[0] != "hidden" {
		t.Fatal("mutable cached usage metadata", again, err)
	}
	for _, expression := range []string{
		`false ? fixed(1) : 1`, `false ? u(param("service_tier")) : 1`,
		`let read = u; read("units")`, `$env["u"]("units")`,
		`u("un" + "its")`, `let u = 1; u`, `false ? p : 1`, `u("")`,
	} {
		if _, err := TaskUsageKeys(expression); err == nil {
			t.Fatalf("unsafe task expression accepted: %s", expression)
		}
	}
}

func TestTaskCurrencySemanticsAndLegacyCacheIsolation(t *testing.T) {
	InvalidateCache()
	for i := 0; i < 3; i++ {
		quote, err := QuoteTaskExpression(taskQuoteInput("1"))
		if err != nil || quote.Amount != 1 || quote.QuotaAfterGroup != 1000 || quote.Profile != TaskExpressionProfile {
			t.Fatal(quote, err)
		}
		legacy, err := ComputeTieredQuota(&BillingSnapshot{ExprString: "1", ExprHash: ExprHashString("1"), ExprVersion: 1, QuotaPerUnit: 1000, GroupRatio: 1}, TokenParams{})
		if err != nil || legacy.ActualQuotaAfterGroup != 0 || legacy.ActualQuotaBeforeGroup != .001 {
			t.Fatal("token /million semantics changed", legacy, err)
		}
		if _, err := TaskUsageKeys(`u("units") * 0.1`); err != nil {
			t.Fatal(err)
		}
		if _, err := CompileFromCache(`u("units") * 0.1`); err == nil {
			t.Fatal("task program leaked into legacy compiler")
		}
		InvalidateCache()
	}
}

func TestTaskQuoteZeroMissingRoundingAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		units  float64
		target int
	}{{0, 0}, {.6, 60}, {.005, 1}} {
		input := taskQuoteInput(`u("units") * 0.1`)
		input.Usage["units"] = tc.units
		quote, err := QuoteTaskExpression(input)
		if err != nil || quote.QuotaAfterGroup != tc.target {
			t.Fatal(tc, quote, err)
		}
	}
	for _, mutate := range []func(*TaskQuoteInput){
		func(in *TaskQuoteInput) { delete(in.Usage, "units") },
		func(in *TaskQuoteInput) { in.Usage["units"] = math.NaN() },
		func(in *TaskQuoteInput) { in.Usage["units"] = math.Inf(1) },
		func(in *TaskQuoteInput) { in.Usage["units"] = -1. },
		func(in *TaskQuoteInput) { in.Profile = "" },
		func(in *TaskQuoteInput) { in.Profile = "legacy-token@1" },
		func(in *TaskQuoteInput) { in.PricingAt = time.Time{} },
		func(in *TaskQuoteInput) { in.QuotaPerUnit = 0 },
		func(in *TaskQuoteInput) { in.GroupRatio = -1 },
		func(in *TaskQuoteInput) { in.GroupRatio = math.Inf(1) },
		func(in *TaskQuoteInput) { in.Expression = "-1" },
		func(in *TaskQuoteInput) { in.Expression = "1 / 0" },
		func(in *TaskQuoteInput) { in.Expression = "2147483.648" },
		func(in *TaskQuoteInput) { in.Expression = "1e300"; in.QuotaPerUnit = 1e300 },
	} {
		in := taskQuoteInput(`u("units") * 0.1`)
		mutate(&in)
		if _, err := QuoteTaskExpression(in); err == nil {
			t.Fatalf("invalid quote input accepted: %+v", in)
		}
	}
	in := taskQuoteInput(`u("units") * 0.1`)
	in.GroupRatio = 0
	if q, err := QuoteTaskExpression(in); err != nil || q.QuotaAfterGroup != 0 || q.Amount != .1 {
		t.Fatal("explicit free group lost", q, err)
	}
}

func TestTaskQuoteFrozenTimeAndExistingRequestAllowlist(t *testing.T) {
	in := taskQuoteInput(`hour("Asia/Shanghai") == 2 ? tier("frozen", u("units") * 0.1) : 0.2`)
	q, err := QuoteTaskExpression(in)
	if err != nil || q.Amount != .1 || q.MatchedTier != "frozen" || !q.PricingAt.Equal(in.PricingAt) {
		t.Fatal(q, err)
	}
	in.PricingAt = in.PricingAt.Add(time.Hour)
	if q, err := QuoteTaskExpression(in); err != nil || q.Amount != .2 {
		t.Fatal(q, err)
	}
	in = taskQuoteInput(`hour("invalid/timezone") == 18 && hour("") == 18 ? 1 : 2`)
	if q, err := QuoteTaskExpression(in); err != nil || q.Amount != 1 {
		t.Fatal(q, err)
	}
	in = taskQuoteInput(`header("authorization") == "" && param("messages.0.content") == nil ? (param("service_tier") == "priority" ? 2 : 1) : 999`)
	in.Request = RequestInput{Headers: map[string]string{"Authorization": "synthetic-test-only"}, Body: []byte(`{"service_tier":"priority","messages":[{"content":"synthetic-test-only"}]}`)}
	if q, err := QuoteTaskExpression(in); err != nil || q.Amount != 2 {
		t.Fatal("request allowlist changed", q, err)
	}
}

func TestTaskAndTokenCacheConcurrentContexts(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				in := taskQuoteInput(`u("units") * 0.1`)
				if q, err := QuoteTaskExpression(in); err != nil || q.QuotaAfterGroup != 100 {
					t.Errorf("task cache: %+v %v", q, err)
				}
				if value, _, err := RunExpr(`p * 2`, TokenParams{P: 3}); err != nil || value != 6 {
					t.Errorf("token cache: %v %v", value, err)
				}
			}
		}()
	}
	wg.Wait()
}

const taskTimezoneProbeEnv = "MAX_API_BILLING_LOCAL_TIMEZONE_PROBE"

// Isolate time.Local changes in child processes so this test cannot alter the
// clock interpretation of other tests or background work in the parent.
func TestTaskTimezonesRejectNodeLocal(t *testing.T) {
	for _, node := range []string{"utc", "utc8"} {
		t.Run(node, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestTaskTimezoneNodeProbe$", "-test.v", "-test.timeout=30s")
			cmd.Env = append(os.Environ(), taskTimezoneProbeEnv+"="+node)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node %s failed: %v\n%s", node, err, output)
			}
			if !strings.Contains(string(output), "TASK_TIMEZONE_PROBE_DONE:"+node) {
				t.Fatalf("node %s probe was not executed: %s", node, output)
			}
			t.Logf("isolated %s probe: %s", node, output)
		})
	}
}

func TestTaskTimezoneNodeProbe(t *testing.T) {
	node := os.Getenv(taskTimezoneProbeEnv)
	if node == "" {
		t.Skip("subprocess-only probe exercised by TestTaskTimezonesRejectNodeLocal")
	}
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	switch node {
	case "utc":
		time.Local = time.UTC
	case "utc8":
		time.Local = time.FixedZone("test-node-utc8", 8*60*60)
	default:
		t.Fatalf("unknown probe node %q", node)
	}
	makeInput := func(expression string) TaskQuoteInput {
		in := taskQuoteInput(expression)
		in.PricingAt = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
		in.Usage["zone"] = "UTC"
		return in
	}
	// Independent controls: at 00:00Z UTC is hour 0 and Shanghai is hour 8,
	// irrespective of the child process's local timezone.
	for _, control := range []struct {
		expression string
		target     int
	}{
		{`hour("UTC") == 0 ? 1 : 2`, 1000},
		{`hour("Asia/Shanghai") == 8 ? 2 : 1`, 2000},
		{`hour("") == 0 && hour("invalid/timezone") == 0 ? 1 : 2`, 1000},
		{`hour(string(u("zone"))) == 0 ? 1 : 2`, 1000},
	} {
		q, err := QuoteTaskExpression(makeInput(control.expression))
		if err != nil || q.QuotaAfterGroup != control.target {
			t.Fatalf("node %s explicit-zone control: %+v %v", node, q, err)
		}
	}
	for _, helper := range []string{"hour", "minute", "weekday", "month", "day"} {
		for _, argument := range []string{`"Local"`, `" Local "`, `"Lo" + "cal"`, `string(u("zone"))`, `string(param("service_tier"))`} {
			expression := helper + "(" + argument + ") == 0 ? 1 : 2"
			in := makeInput(expression)
			in.Usage["zone"] = " Local "
			in.Request.Body = []byte(`{"service_tier":" Local "}`)
			q, err := QuoteTaskExpression(in)
			if err == nil {
				t.Errorf("node %s accepted %s: amount=%v quota=%d", node, expression, q.Amount, q.QuotaAfterGroup)
			} else if !strings.Contains(err.Error(), "Local") {
				t.Errorf("unexpected rejection: %v", err)
			}
		}
		if _, err := TaskUsageKeys("false ? " + helper + `("Local") : 1`); err == nil {
			t.Errorf("hidden literal Local accepted for %s", helper)
		}
	}
	// The legacy entry retains its existing Local support after task rejection.
	value, _, err := RunExpr(`hour("Local") >= 0 ? 1 : 2`, TokenParams{})
	if err != nil || value != 1 {
		t.Errorf("legacy Local behavior changed: %v %v", value, err)
	}
	t.Log("TASK_TIMEZONE_PROBE_DONE:" + node)
}

func TestTaskRejectsUnfrozenTimeObjectBuiltins(t *testing.T) {
	// Warm the legacy cache first: its program must not bypass task validation.
	if _, err := CompileFromCache(`now().Year()`); err != nil {
		t.Fatal("legacy builtin support changed", err)
	}
	for _, expression := range []string{
		`now().Year()`,
		`date("2026-10-30T00:00:00Z").Local().Hour()`,
		`timezone("Local").String() == "Local" ? 1 : 2`,
		`false ? now().Year() : 1`,
		`false ? date("2026-10-30T00:00:00Z").Local().Hour() : 1`,
		`false ? (timezone("Local").String() == "Local" ? 1 : 2) : 1`,
	} {
		if _, err := TaskUsageKeys(expression); err == nil {
			t.Errorf("ambient time builtin accepted during validation: %s", expression)
		} else if !strings.Contains(err.Error(), "use frozen") {
			t.Errorf("unexpected validation rejection for %s: %v", expression, err)
		}
		in := taskQuoteInput(expression)
		in.PricingAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		q, err := QuoteTaskExpression(in)
		if err == nil {
			t.Errorf("ambient time builtin accepted during quote: %s amount=%v quota=%d", expression, q.Amount, q.QuotaAfterGroup)
		} else if !strings.Contains(err.Error(), "use frozen") {
			t.Errorf("unexpected quote rejection for %s: %v", expression, err)
		}
	}
	// Deterministic builtins and local variables retain their scalar semantics.
	if q, err := QuoteTaskExpression(taskQuoteInput(`let base = 1; base + abs(-1)`)); err != nil || q.QuotaAfterGroup != 2000 {
		t.Fatal("deterministic task expression rejected", q, err)
	}
	// Task rejection must not evict or restrict the existing token language.
	if _, err := CompileFromCache(`now().Year()`); err != nil {
		t.Fatal("legacy builtin support changed", err)
	}
}
