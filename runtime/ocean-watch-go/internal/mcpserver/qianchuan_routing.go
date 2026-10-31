package mcpserver

import applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"

type routedPreflightOutput struct {
	OK        bool                                  `json:"ok"`
	RequestID string                                `json:"request_id"`
	Mode      string                                `json:"mode"`
	Channel   string                                `json:"channel"`
	Routed    bool                                  `json:"routed"`
	Counts    map[string]int                        `json:"counts"`
	Batches   []routedPreflightBatch                `json:"batches"`
	Pending   []applicationqianchuan.RoutingPending `json:"pending"`
}

type routedPreflightBatch struct {
	InputIndexes []int             `json:"input_indexes"`
	Template     preflightTemplate `json:"template"`
	Status       string            `json:"status"`
	FailureStage string            `json:"failure_stage,omitempty"`
	Preflight    *preflightOutput  `json:"preflight,omitempty"`
	Failure      *errorEnvelope    `json:"failure,omitempty"`
}

func presentRoutedPreflightOutput(requestID string, result applicationqianchuan.BatchCommandResult) routedPreflightOutput {
	output := routedPreflightOutput{OK: true, RequestID: requestID, Mode: "dry_run", Channel: "qianchuan", Routed: true,
		Counts: cloneCounts(result.Counts), Batches: []routedPreflightBatch{}, Pending: append([]applicationqianchuan.RoutingPending{}, result.Pending...)}
	for index := range output.Pending {
		if output.Pending[index].Candidates == nil {
			output.Pending[index].Candidates = []applicationqianchuan.RoutingCandidate{}
		}
	}
	for _, batch := range result.Batches {
		entry := routedPreflightBatch{InputIndexes: append([]int(nil), batch.InputIndexes...),
			Template: preflightTemplate{TemplateID: batch.Template.TemplateID, Name: batch.Template.Name, AdvertiserID: batch.Template.AdvertiserID, ProductIDs: append([]string(nil), batch.Template.ProductIDs...)},
			Status:   batch.Status, FailureStage: batch.FailureStage}
		if batch.Result != nil {
			preflight := presentPreflightOutput(requestID, *batch.Result)
			entry.Preflight = &preflight
		}
		if batch.Cause != nil {
			failure := mapQianchuanPreflightError(batch.Cause)
			entry.Failure = &errorEnvelope{OK: false, RequestID: requestID, Error: errorPayload{Code: failure.Code, Message: failure.Message, Retryable: failure.Retryable, Details: failure.Details}}
		}
		output.Batches = append(output.Batches, entry)
	}
	return output
}
