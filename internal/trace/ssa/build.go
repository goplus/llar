package ssa

import (
	"slices"
	"sort"

	"github.com/goplus/llar/internal/trace"
)

func Build(records []trace.Record, events []trace.Event) Graph {
	return BuildGraph(BuildInput{
		Records: records,
		Events:  events,
	})
}

func buildFromObservation(observation observation) Graph {
	graph := Graph{
		Nodes:        cloneNodes(observation.Nodes),
		Parent:       slices.Clone(observation.Parent),
		Paths:        clonePaths(observation.Paths),
		Deps:         cloneDeps(observation.Deps),
		ActionReads:  make([][]Read, len(observation.Nodes)),
		ActionWrites: make([][]PathState, len(observation.Nodes)),
		ReadersByDef: make(map[PathState][]int),
		InitialDefs:  make(map[string]PathState),
		DefsByPath:   make(map[string][]PathState),
	}
	order := newCausalOrder(graph)
	versionByPath := make(map[string]int)
	missingDefByPath := make(map[string]PathState)
	for i, node := range graph.Nodes {
		deletes := actionDeleteSet(node)
		for _, entry := range node.Env {
			path := envStatePathFromEntry(entry)
			if !pathAllowed(graph.Paths, path) || path == "" {
				continue
			}
			initial, ok := graph.InitialDefs[path]
			if !ok {
				initial = PathState{Writer: -1, Path: path, Version: 0}
				graph.InitialDefs[path] = initial
			}
			graph.ActionReads[i] = append(graph.ActionReads[i], Read{
				Path: path,
				Defs: []PathState{initial},
			})
			graph.ReadersByDef[initial] = append(graph.ReadersByDef[initial], i)
		}
		for _, read := range node.Reads {
			if !pathAllowed(graph.Paths, read) || read == "" {
				continue
			}
			defs := reachingDefsForRead(&order, graph.DefsByPath[read], i)
			if len(defs) == 0 {
				initial, ok := graph.InitialDefs[read]
				if !ok {
					initial = PathState{Writer: -1, Path: read, Version: 0}
					graph.InitialDefs[read] = initial
				}
				defs = []PathState{initial}
			}
			binding := Read{Path: read, Defs: slices.Clone(defs)}
			graph.ActionReads[i] = append(graph.ActionReads[i], binding)
			for _, def := range defs {
				graph.ReadersByDef[def] = append(graph.ReadersByDef[def], i)
			}
		}
		for _, miss := range node.ReadMisses {
			if !pathAllowed(graph.Paths, miss) || miss == "" {
				continue
			}
			def, ok := missingDefByPath[miss]
			if !ok {
				def = PathState{Writer: -1, Path: miss, Version: 0, Missing: true}
				missingDefByPath[miss] = def
				graph.DefsByPath[miss] = append(graph.DefsByPath[miss], def)
			}
			graph.ActionReads[i] = append(graph.ActionReads[i], Read{
				Path: miss,
				Defs: []PathState{def},
			})
			graph.ReadersByDef[def] = append(graph.ReadersByDef[def], i)
		}
		seenWrites := make(map[string]int)
		for _, write := range node.Writes {
			if !pathAllowed(graph.Paths, write) || write == "" {
				continue
			}
			if pos, exists := seenWrites[write]; exists {
				if _, tombstone := deletes[write]; tombstone {
					def := graph.ActionWrites[i][pos]
					def.Tombstone = true
					graph.ActionWrites[i][pos] = def
					defs := graph.DefsByPath[write]
					for j := range defs {
						if defs[j].Writer == def.Writer && defs[j].Version == def.Version {
							defs[j].Tombstone = true
							break
						}
					}
					graph.DefsByPath[write] = defs
				}
				continue
			}
			versionByPath[write]++
			def := PathState{
				Writer:    i,
				Path:      write,
				Version:   versionByPath[write],
				Tombstone: hasPath(deletes, write),
			}
			seenWrites[write] = len(graph.ActionWrites[i])
			graph.ActionWrites[i] = append(graph.ActionWrites[i], def)
			graph.DefsByPath[write] = append(graph.DefsByPath[write], def)
		}
	}
	return graph
}

type causalOrder struct {
	graph     Graph
	descCache map[int]map[int]struct{}
}

func newCausalOrder(graph Graph) causalOrder {
	return causalOrder{
		graph:     graph,
		descCache: make(map[int]map[int]struct{}),
	}
}

func (order *causalOrder) causallyBefore(left, right int) bool {
	if left < 0 || right < 0 || left >= right {
		return false
	}
	leftNode := order.graph.Nodes[left]
	rightNode := order.graph.Nodes[right]
	if leftNode.PID == 0 || rightNode.PID == 0 {
		return false
	}
	if leftNode.PID == rightNode.PID {
		return true
	}
	for parent := order.parent(right); parent >= 0; parent = order.parent(parent) {
		if parent == left {
			return true
		}
	}
	if _, ok := order.descendants(left)[right]; ok {
		return true
	}
	return false
}

func (order *causalOrder) descendants(idx int) map[int]struct{} {
	if out, ok := order.descCache[idx]; ok {
		return out
	}
	seen := make(map[int]struct{})
	stack := []int{idx}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, edge := range order.graph.Deps[cur] {
			if _, ok := seen[edge.To]; ok {
				continue
			}
			seen[edge.To] = struct{}{}
			stack = append(stack, edge.To)
		}
	}
	order.descCache[idx] = seen
	return seen
}

func (order *causalOrder) parent(idx int) int {
	if idx < 0 || idx >= len(order.graph.Parent) {
		return -1
	}
	return order.graph.Parent[idx]
}

func reachingDefsForRead(order *causalOrder, defs []PathState, reader int) []PathState {
	if len(defs) == 0 {
		return nil
	}
	candidates := make([]PathState, 0, len(defs))
	writerIndexes := make([]int, 0, len(defs))
	for _, def := range defs {
		if def.Writer < 0 || def.Writer >= reader {
			continue
		}
		candidates = append(candidates, def)
		writerIndexes = append(writerIndexes, def.Writer)
	}
	if len(candidates) == 0 {
		return nil
	}
	if readUsesLinearLatest(order.graph, writerIndexes, reader) {
		latest := candidates[0]
		for _, candidate := range candidates[1:] {
			if candidate.Writer > latest.Writer {
				latest = candidate
				continue
			}
			if candidate.Writer == latest.Writer && candidate.Version > latest.Version {
				latest = candidate
			}
		}
		return []PathState{latest}
	}
	latest := make([]PathState, 0, len(candidates))
	for _, candidate := range candidates {
		superseded := false
		for _, other := range candidates {
			if candidate == other {
				continue
			}
			if order.causallyBefore(candidate.Writer, other.Writer) {
				superseded = true
				break
			}
		}
		if superseded {
			continue
		}
		latest = append(latest, candidate)
	}
	if len(latest) == 0 {
		return nil
	}
	sort.Slice(latest, func(i, j int) bool {
		if latest[i].Writer != latest[j].Writer {
			return latest[i].Writer < latest[j].Writer
		}
		if latest[i].Version != latest[j].Version {
			return latest[i].Version < latest[j].Version
		}
		if latest[i].Path != latest[j].Path {
			return latest[i].Path < latest[j].Path
		}
		if latest[i].Tombstone == latest[j].Tombstone {
			return false
		}
		return !latest[i].Tombstone && latest[j].Tombstone
	})
	return latest
}

func readUsesLinearLatest(graph Graph, candidates []int, reader int) bool {
	if reader < 0 || reader >= len(graph.Nodes) {
		return true
	}
	if graph.Nodes[reader].PID == 0 {
		return true
	}
	for _, writer := range candidates {
		if writer < 0 || writer >= len(graph.Nodes) || graph.Nodes[writer].PID == 0 {
			return true
		}
	}
	return false
}

func pathAllowed(paths map[string]PathInfo, path string) bool {
	if len(paths) == 0 {
		return false
	}
	_, ok := paths[path]
	return ok
}

func actionDeleteSet(node ExecNode) map[string]struct{} {
	if len(node.Deletes) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(node.Deletes))
	for _, path := range node.Deletes {
		if path == "" {
			continue
		}
		out[path] = struct{}{}
	}
	return out
}

func hasPath(paths map[string]struct{}, path string) bool {
	if len(paths) == 0 {
		return false
	}
	_, ok := paths[path]
	return ok
}

func cloneNodes(src []ExecNode) []ExecNode {
	if len(src) == 0 {
		return nil
	}
	out := make([]ExecNode, len(src))
	for i, node := range src {
		out[i] = ExecNode{
			PID:          node.PID,
			ParentPID:    node.ParentPID,
			Argv:         slices.Clone(node.Argv),
			Cwd:          node.Cwd,
			Env:          slices.Clone(node.Env),
			Reads:        slices.Clone(node.Reads),
			ReadMisses:   slices.Clone(node.ReadMisses),
			Writes:       slices.Clone(node.Writes),
			Deletes:      slices.Clone(node.Deletes),
			ExecPath:     node.ExecPath,
			Tool:         node.Tool,
			Kind:         node.Kind,
			ActionKey:    node.ActionKey,
			StructureKey: node.StructureKey,
			Fingerprint:  node.Fingerprint,
		}
	}
	return out
}

func clonePaths(src map[string]PathInfo) map[string]PathInfo {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]PathInfo, len(src))
	for path, info := range src {
		out[path] = PathInfo{
			Path:    info.Path,
			Writers: slices.Clone(info.Writers),
			Readers: slices.Clone(info.Readers),
			Role:    info.Role,
		}
	}
	return out
}

func cloneDeps(src [][]ExecEdge) [][]ExecEdge {
	if len(src) == 0 {
		return nil
	}
	out := make([][]ExecEdge, len(src))
	for i, edges := range src {
		out[i] = slices.Clone(edges)
	}
	return out
}
