package personalusage

import "testing"

func TestExecutionIdentityScope(t *testing.T) {
	var missing *Identity
	if missing.Scope() != "" || (&Identity{Subagent: "guardian"}).Scope() != "" {
		t.Fatal("missing thread must not join parent scope")
	}
	main := (&Identity{ThreadID: "child", Kind: "turn"}).Scope()
	if main != (&Identity{ThreadID: "child", Kind: "compaction"}).Scope() {
		t.Fatal("compaction stays adjacent to the turn it invalidates")
	}
	if main == (&Identity{ThreadID: "parent"}).Scope() || main == (&Identity{ThreadID: "child", Kind: "memory"}).Scope() {
		t.Fatal("independent execution scopes merged")
	}
}
