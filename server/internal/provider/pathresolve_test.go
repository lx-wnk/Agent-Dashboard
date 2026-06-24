package provider

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestResolvePath_Scalar(t *testing.T) {
	m := decode(t, `{"payload":{"info":{"total_token_usage":{"input_tokens":31751}}}}`)
	got := resolvePath(m, "payload.info.total_token_usage.input_tokens")
	if len(got) != 1 || toFloat(got[0]) != 31751 {
		t.Fatalf("want [31751], got %v", got)
	}
}

func TestResolvePath_Missing(t *testing.T) {
	m := decode(t, `{"payload":{}}`)
	if got := resolvePath(m, "payload.info.input"); len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestResolvePath_ArrayIteration(t *testing.T) {
	m := decode(t, `{"event":{"agentEvent":{"modelUsage":[{"cost":0.01},{"cost":0.02}]}}}`)
	got := resolvePath(m, "event.agentEvent.modelUsage[].cost")
	if len(got) != 2 || toFloat(got[0]) != 0.01 || toFloat(got[1]) != 0.02 {
		t.Fatalf("want [0.01 0.02], got %v", got)
	}
}

func TestResolveFirst_FallbackList(t *testing.T) {
	m := decode(t, `{"modelUsage":[{"inputTokens":10}]}`)
	got := resolveFirst(m, []string{"modelUsage[].input", "modelUsage[].inputTokens"})
	if len(got) != 1 || toFloat(got[0]) != 10 {
		t.Fatalf("want [10], got %v", got)
	}
}

func TestToFloat_StringAndNumber(t *testing.T) {
	if toFloat(float64(5)) != 5 || toFloat("7") != 7 || toFloat(nil) != 0 {
		t.Fatal("toFloat conversions wrong")
	}
}
