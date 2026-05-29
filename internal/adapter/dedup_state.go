package adapter

// TurnDedupState 仅保存回合去重字段（P1：与 binding 分离）。
type TurnDedupState struct {
	LastTaskSummary string `json:"last_task_summary,omitempty"`
	LastTurnID      string `json:"last_turn_id,omitempty"`
	LastTurnSig     string `json:"last_turn_sig,omitempty"`
}
