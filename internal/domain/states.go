package domain

import "fmt"

// 状态机转换表（docs/ARCHITECTURE.md §5 Placement 状态机 + source_files/tasks 语义）。
// 变更转换关系属于设计决策，须先修订 §5 再改本表。

var placementTransitions = map[string][]string{
	PlacementProposed:      {PlacementAutoApproved, PlacementPendingReview},
	PlacementPendingReview: {PlacementApproved, PlacementRejected},
	PlacementAutoApproved:  {PlacementRework},
	PlacementApproved:      {PlacementRework},
	PlacementRejected:      {PlacementRework},
	PlacementRework:        {PlacementProposed},
}

var sourceFileTransitions = map[string][]string{
	SourceFileNew:     {SourceFileParsing, SourceFileIgnored},
	SourceFileParsing: {SourceFileParsed, SourceFileError, SourceFileIgnored},
	SourceFileParsed:  {SourceFilePlaced, SourceFileIgnored, SourceFileParsing},
	SourceFileError:   {SourceFileParsing},
	SourceFilePlaced:  {SourceFileParsed},
	SourceFileIgnored: {SourceFileParsing},
}

var taskTransitions = map[string][]string{
	TaskQueued:  {TaskRunning, TaskCancelled},
	TaskRunning: {TaskDone, TaskFailed, TaskCancelled},
}

var reviewCaseTransitions = map[string][]string{
	ReviewOpen: {ReviewApproved, ReviewRejected, ReviewReworked},
}

// CanTransition 判断状态机 from→to 是否合法。
func CanTransition(table map[string][]string, from, to string) bool {
	for _, t := range table[from] {
		if t == to {
			return true
		}
	}
	return false
}

func PlacementTransitionOK(from, to string) error {
	if !CanTransition(placementTransitions, from, to) {
		return fmt.Errorf("placements: 非法状态转换 %s → %s", from, to)
	}
	return nil
}

func SourceFileTransitionOK(from, to string) error {
	if !CanTransition(sourceFileTransitions, from, to) {
		return fmt.Errorf("source_files: 非法状态转换 %s → %s", from, to)
	}
	return nil
}

func TaskTransitionOK(from, to string) error {
	if !CanTransition(taskTransitions, from, to) {
		return fmt.Errorf("tasks: 非法状态转换 %s → %s", from, to)
	}
	return nil
}

func ReviewCaseTransitionOK(from, to string) error {
	if !CanTransition(reviewCaseTransitions, from, to) {
		return fmt.Errorf("review_cases: 非法状态转换 %s → %s", from, to)
	}
	return nil
}

var slotTypes = map[string]bool{
	SlotEpisode: true, SlotSpecial: true, SlotMovie: true,
	SlotOP: true, SlotED: true, SlotPV: true, SlotCM: true,
	SlotExtra: true, SlotSub: true, SlotIgnored: true,
}

func ValidSlotType(s string) bool { return slotTypes[s] }

var decisionSources = map[string]bool{
	DecisionRule: true, DecisionLLM: true, DecisionHuman: true,
}

func ValidDecisionSource(s string) bool { return decisionSources[s] }
