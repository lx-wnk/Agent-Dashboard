package pipeline

import (
	"strings"
	"testing"
)

type fakeKindHandler struct{ stage string }

func (f fakeKindHandler) Stage() string       { return f.stage }
func (f fakeKindHandler) RequiresAgent() bool { return false }
func (f fakeKindHandler) Execute(*StageContext) (StageTransition, error) {
	return NextTransition{}, nil
}

// A module may add a kind of stage, never redefine one of the core's. The core
// stages carry the lifecycle every task depends on; a module silently taking
// one over would change what "implementation" means for every task at once.
func TestRegisterStageKind_RefusesACoreStageName(t *testing.T) {
	o := &PipelineOrchestrator{}

	if err := o.RegisterStageKind("gather", fakeKindHandler{stage: "gather"}); err != nil {
		t.Fatalf("a new kind must register: %v", err)
	}
	if got := o.resolveHandler("gather"); got == nil {
		t.Fatal("the registered kind is not resolvable")
	}

	for _, core := range StageOrder {
		err := o.RegisterStageKind(core, fakeKindHandler{stage: core})
		if err == nil {
			t.Fatalf("registering the core stage %q must be refused", core)
		}
		if !strings.Contains(err.Error(), core) {
			t.Errorf("error %q does not name the stage it refused", err)
		}
	}
}
