package evaluator

import (
	"strings"

	"github.com/goplus/llar/internal/trace"
	tracessa "github.com/goplus/llar/internal/trace/ssa"
)

func buildGraph(records []trace.Record) tracessa.Graph {
	return buildGraphWithScope(records, trace.Scope{})
}

func buildGraphWithScope(records []trace.Record, scope trace.Scope) tracessa.Graph {
	return buildGraphWithScopeAndDigests(records, scope, nil)
}

func buildGraphWithScopeAndDigests(records []trace.Record, scope trace.Scope, inputDigests map[string]string) tracessa.Graph {
	return tracessa.BuildGraph(tracessa.BuildInput{
		Records:      records,
		Scope:        scope,
		InputDigests: inputDigests,
	})
}

func buildGraphWithEvents(events []trace.Event) tracessa.Graph {
	return buildGraphWithEventsAndDigests(events, trace.Scope{}, nil)
}

func buildGraphWithEventsAndDigests(events []trace.Event, scope trace.Scope, inputDigests map[string]string) tracessa.Graph {
	return tracessa.BuildGraph(tracessa.BuildInput{
		Events:       events,
		Scope:        scope,
		InputDigests: inputDigests,
	})
}

func collectExecPaths(actions []tracessa.ExecNode) map[string]struct{} {
	executed := make(map[string]struct{})
	for _, action := range actions {
		if action.ExecPath == "" {
			continue
		}
		executed[action.ExecPath] = struct{}{}
	}
	return executed
}

func actionWritesExecutedPath(action tracessa.ExecNode, executedPaths map[string]struct{}) bool {
	if len(executedPaths) == 0 {
		return false
	}
	for _, path := range action.Writes {
		if _, ok := executedPaths[path]; ok {
			return true
		}
	}
	return false
}

func isExplicitDeliveryPath(path string, scope trace.Scope) bool {
	root := strings.TrimSuffix(normalizePath(scope.InstallRoot), "/")
	if root == "" {
		return false
	}
	path = normalizePath(path)
	return path == root || strings.HasPrefix(path, root+"/")
}

func isDeliveryPath(actions []tracessa.ExecNode, outdeg []int, executedPaths map[string]struct{}, facts tracessa.PathInfo) bool {
	for _, writer := range facts.Writers {
		if writer < 0 || writer >= len(actions) {
			continue
		}
		action := actions[writer]
		if (action.Kind == tracessa.KindCopy || action.Kind == tracessa.KindInstall) && outdeg[writer] == 0 && !actionWritesExecutedPath(action, executedPaths) {
			return true
		}
	}
	return false
}
