package domain

import "testing"

func TestPlacementStateMachine(t *testing.T) {
	legal := [][2]string{
		{PlacementProposed, PlacementAutoApproved},
		{PlacementProposed, PlacementPendingReview},
		{PlacementPendingReview, PlacementApproved},
		{PlacementPendingReview, PlacementRejected},
		{PlacementAutoApproved, PlacementRework},
		{PlacementApproved, PlacementRework},
		{PlacementRejected, PlacementRework},
		{PlacementRework, PlacementProposed},
	}
	for _, p := range legal {
		if err := PlacementTransitionOK(p[0], p[1]); err != nil {
			t.Errorf("expected legal %s→%s: %v", p[0], p[1], err)
		}
	}
	illegal := [][2]string{
		{PlacementProposed, PlacementApproved},          // 未经审核不得直接批准
		{PlacementProposed, PlacementRework},            // 未落地无从返工
		{PlacementApproved, PlacementRejected},          // 批准不可事后驳回（须走返工）
		{PlacementAutoApproved, PlacementPendingReview}, // 已放行不进队列
		{PlacementRework, PlacementApproved},            // 返工必须回 proposed
		{PlacementApproved, PlacementAutoApproved},
	}
	for _, p := range illegal {
		if err := PlacementTransitionOK(p[0], p[1]); err == nil {
			t.Errorf("expected illegal %s→%s", p[0], p[1])
		}
	}
}

func TestSourceFileStateMachine(t *testing.T) {
	if err := SourceFileTransitionOK(SourceFileNew, SourceFileParsing); err != nil {
		t.Errorf("new→parsing should be legal: %v", err)
	}
	if err := SourceFileTransitionOK(SourceFileParsed, SourceFilePlaced); err != nil {
		t.Errorf("parsed→placed should be legal: %v", err)
	}
	if err := SourceFileTransitionOK(SourceFilePlaced, SourceFileParsed); err != nil {
		t.Errorf("placed→parsed (返工回退) should be legal: %v", err)
	}
	if err := SourceFileTransitionOK(SourceFileNew, SourceFilePlaced); err == nil {
		t.Error("new→placed should be illegal")
	}
	if err := SourceFileTransitionOK(SourceFilePlaced, SourceFileIgnored); err == nil {
		t.Error("placed→ignored should be illegal (先回退再忽略)")
	}
}

func TestTaskStateMachine(t *testing.T) {
	if err := TaskTransitionOK(TaskQueued, TaskRunning); err != nil {
		t.Errorf("queued→running should be legal: %v", err)
	}
	if err := TaskTransitionOK(TaskRunning, TaskDone); err != nil {
		t.Errorf("running→done should be legal: %v", err)
	}
	if err := TaskTransitionOK(TaskDone, TaskRunning); err == nil {
		t.Error("done→running should be illegal")
	}
	if err := TaskTransitionOK(TaskQueued, TaskCancelled); err != nil {
		t.Errorf("queued→cancelled should be legal: %v", err)
	}
	if err := TaskTransitionOK(TaskDone, TaskCancelled); err == nil {
		t.Error("done→cancelled should be illegal")
	}
}

func TestReviewCaseStateMachine(t *testing.T) {
	if err := ReviewCaseTransitionOK(ReviewOpen, ReviewApproved); err != nil {
		t.Errorf("open→approved should be legal: %v", err)
	}
	if err := ReviewCaseTransitionOK(ReviewApproved, ReviewOpen); err == nil {
		t.Error("approved→open should be illegal (重开=新工单)")
	}
}

func TestSlotTypeAndDecisionSource(t *testing.T) {
	for _, s := range []string{SlotEpisode, SlotSpecial, SlotMovie, SlotOP, SlotED, SlotPV, SlotCM, SlotExtra, SlotSub, SlotIgnored} {
		if !ValidSlotType(s) {
			t.Errorf("slot type %s should be valid", s)
		}
	}
	if ValidSlotType("trailer") {
		t.Error("trailer is not a slot type (归 pv/cm/extra)")
	}
	if !ValidDecisionSource(DecisionRule) || ValidDecisionSource("magic") {
		t.Error("decision source validation broken")
	}
}
