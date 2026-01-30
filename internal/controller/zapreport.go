package controller

import (
	"context"
	"encoding/json"

	batchv1 "k8s.io/api/batch/v1"
)

func (r *ScanReconciler) collectAlertsFromPodReport(ctx context.Context, job *batchv1.Job) (*parsedAlerts, error) {
	pods, err := r.podsForJob(ctx, job)
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return &parsedAlerts{}, nil
	}

	acc := map[string]*pluginAlert{}
	all := parsedAlerts{Total: 0}

	picked := false

	for _, p := range pods.Items {
		// We need the report file, but exec does not work against terminated containers.
		// Prefer an active pod (Running) if one exists.
		if p.Status.Phase != "Running" {
			continue
		}
		picked = true

		eg := r.execGetter
		if eg == nil {
			eg = r
		}
		b, err := eg.readFileFromPod(ctx, p.Namespace, p.Name, "zap", "/zap/wrk/zap.json")
		if err != nil {
			return nil, err
		}

		var report zapJSONReport
		if err := json.Unmarshal(b, &report); err != nil {
			return nil, err
		}

		for _, site := range report.Site {
			for _, a := range site.Alerts {
				all.Total++
				pid := a.PluginID
				risk := normalizeRisk(a.RiskCode)
				key := pid + ":" + risk
				pa, ok := acc[key]
				if !ok {
					pa = &pluginAlert{PluginID: pid, Risk: risk}
					acc[key] = pa
				}
				pa.Count++
			}
		}
	}

	if !picked {
		return &parsedAlerts{}, nil
	}

	for _, v := range acc {
		all.ByPlugin = append(all.ByPlugin, *v)
	}

	return &all, nil
}
