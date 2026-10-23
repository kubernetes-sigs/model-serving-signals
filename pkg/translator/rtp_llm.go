/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package translator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

const (
	rtpRequestsQueued  = "rtp_llm_worker_status_requests_queued"
	rtpRequestsRunning = "rtp_llm_worker_status_requests_running"
)

// RTPWorkerStatus scrapes the RTP-LLM frontend's JSON /worker_status endpoint.
// The C++ HTTP health endpoint with the same path does not contain task states.
// HTTP supplies the URL, optional client, and context-aware transport.
type RTPWorkerStatus struct {
	HTTP
}

var _ Scraper = (*RTPWorkerStatus)(nil)

// Scrape derives per-DP request gauges from the frontend's task snapshots.
func (s *RTPWorkerStatus) Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	return s.scrape(ctx, "application/json", parseRTPWorkerStatus)
}

type rtpWorkerStatus struct {
	Alive  bool         `json:"alive"`
	DPRank *json.Number `json:"dp_rank"`
	DPSize int          `json:"dp_size"`
	Role   string       `json:"role_type"`
	Tasks  *[]rtpTask   `json:"running_task_info"`
}

type rtpTask struct {
	Phase string `json:"phase"`
}

func parseRTPWorkerStatus(r io.Reader) (map[string]*dto.MetricFamily, error) {
	// The top level repeats the first worker for compatibility. Only results
	// is authoritative; reading both would count the first DP twice.
	var response struct {
		Error   json.RawMessage   `json:"error"`
		Results []rtpWorkerStatus `json:"results"`
	}
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decoding RTP-LLM worker status: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("unexpected data after RTP-LLM worker status")
	}
	if len(response.Error) != 0 || len(response.Results) == 0 {
		return nil, errors.New("RTP-LLM worker status contains an error or no DP results")
	}

	families := map[string]*dto.MetricFamily{}
	for _, name := range []string{rtpRequestsQueued, rtpRequestsRunning} {
		families[name] = &dto.MetricFamily{Name: proto.String(name), Type: dto.MetricType_GAUGE.Enum()}
	}
	ranks := make(map[int64]bool, len(response.Results))
	for _, worker := range response.Results {
		if !worker.Alive || worker.DPRank == nil || worker.Tasks == nil {
			return nil, errors.New("RTP-LLM worker is not alive or lacks dp_rank/running_task_info")
		}
		// The frontend skips unreachable workers and overwrites its top-level
		// dp_size with the result count. Use each backend's configured dp_size
		// to reject partial snapshots instead of reporting an apparent load drop.
		if worker.DPSize != len(response.Results) {
			return nil, fmt.Errorf("incomplete RTP-LLM worker status: got %d DP results, expected %d", len(response.Results), worker.DPSize)
		}
		rank, err := worker.DPRank.Int64()
		if err != nil || rank < 0 || rank >= int64(worker.DPSize) || ranks[rank] {
			return nil, fmt.Errorf("invalid or duplicate RTP-LLM DP rank %q", *worker.DPRank)
		}
		ranks[rank] = true
		switch worker.Role {
		case "ROLE_TYPE_PDFUSION", "ROLE_TYPE_PREFILL", "ROLE_TYPE_DECODE":
		default:
			return nil, fmt.Errorf("unsupported RTP-LLM worker role %q", worker.Role)
		}

		var queued, running float64
		for _, task := range *worker.Tasks {
			switch task.Phase {
			case "TASK_PHASE_RECEIVED", "TASK_PHASE_KV_ALLOCATED":
				queued++
			case "TASK_PHASE_RUNNING":
				running++
			case "TASK_PHASE_PENDING":
				// No local GenerateStream exists. These can be cancellation
				// control records, not requests in this engine's scheduler.
			default:
				return nil, fmt.Errorf("unsupported or missing RTP-LLM task phase %q", task.Phase)
			}
		}
		labels := []*dto.LabelPair{
			{Name: proto.String("dp_rank"), Value: proto.String(strconv.FormatInt(rank, 10))},
			{Name: proto.String("role_type"), Value: proto.String(worker.Role)},
		}
		for name, value := range map[string]float64{rtpRequestsQueued: queued, rtpRequestsRunning: running} {
			families[name].Metric = append(families[name].Metric, &dto.Metric{
				Label: labels,
				Gauge: &dto.Gauge{Value: proto.Float64(value)},
			})
		}
	}
	return families, nil
}
