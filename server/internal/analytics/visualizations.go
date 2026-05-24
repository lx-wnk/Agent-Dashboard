package analytics

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lx-wnk/agent-dashboard/sdk"
)

// maxCoOccurrenceTools caps the matrix dimension returned by
// BuildCoOccurrence. Excess tools are dropped after sorting by session
// count and Meta.Truncated is flipped on.
const maxCoOccurrenceTools = 50

// BuildSankey aggregates consecutive (prev → curr) tool transitions
// across all sessions discovered by ScanOpts. Nodes are the unique tool
// names that participated in at least one transition; links carry the
// summed transition count.
func BuildSankey(ctx context.Context, opts ScanOpts, dirs []string) (sdk.SankeyData, error) {
	byID, err := scanSessionsForTools(ctx, opts, dirs)
	if err != nil {
		return sdk.SankeyData{}, err
	}
	linkCounts := make(map[string]int)
	nodeNames := make(map[string]bool)
	totalCalls := 0
	for _, calls := range byID {
		totalCalls += len(calls)
		for i := 1; i < len(calls); i++ {
			prev := calls[i-1].Name
			curr := calls[i].Name
			nodeNames[prev] = true
			nodeNames[curr] = true
			linkCounts[prev+"\x00"+curr]++
		}
	}

	names := make([]string, 0, len(nodeNames))
	for n := range nodeNames {
		names = append(names, n)
	}
	sort.Strings(names)
	nodes := make([]sdk.SankeyNode, 0, len(names))
	for _, n := range names {
		nodes = append(nodes, sdk.SankeyNode{ID: n, Name: n})
	}

	links := make([]sdk.SankeyLink, 0, len(linkCounts))
	for key, count := range linkCounts {
		idx := strings.IndexByte(key, 0)
		links = append(links, sdk.SankeyLink{
			Source: key[:idx],
			Target: key[idx+1:],
			Value:  count,
		})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Source != links[j].Source {
			return links[i].Source < links[j].Source
		}
		return links[i].Target < links[j].Target
	})

	return sdk.SankeyData{
		Nodes: nodes,
		Links: links,
		Meta: sdk.SankeyMeta{
			SessionCount: len(byID),
			CallCount:    totalCalls,
		},
	}, nil
}

// BuildDAG walks one session JSONL end-to-end and emits chronological
// node + edge data. ScanOpts.Sessions must contain exactly one entry; the
// helper looks the file up via DiscoverSessions to keep config-dir
// resolution centralized.
func BuildDAG(ctx context.Context, opts ScanOpts, dirs []string) (sdk.DAGData, error) {
	if len(opts.Sessions) != 1 || opts.Sessions[0] == "" {
		return sdk.DAGData{}, errors.New("dag requires exactly one session id")
	}
	files := DiscoverSessions(opts, dirs)
	if len(files) == 0 {
		return sdk.DAGData{}, fmt.Errorf("session %s not found", opts.Sessions[0])
	}
	if err := ctx.Err(); err != nil {
		return sdk.DAGData{}, err
	}
	return readDAGFromFile(files[0].Path)
}

// readDAGFromFile streams a single JSONL file and produces nodes for
// each assistant/user message and each tool_use block, plus chrono edges
// between consecutive nodes and result edges from tool_use → tool_result.
func readDAGFromFile(path string) (sdk.DAGData, error) {
	f, err := os.Open(path)
	if err != nil {
		return sdk.DAGData{}, err
	}
	defer f.Close() //nolint:errcheck

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var nodes []sdk.DAGNode
	var links []sdk.DAGLink
	// Track tool_use_id → node ID so tool_result lines can wire back.
	toolUseNodeID := make(map[string]string)
	lineIdx := 0
	prevNodeID := ""

	type dagEnv struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
	}
	type dagMsg struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	type dagBlock struct {
		Type       string `json:"type"`
		Name       string `json:"name"`
		ID         string `json:"id"`
		Text       string `json:"text"`
		ToolUseID  string `json:"tool_use_id"`
		ContentTxt string `json:"content"`
	}

	addNode := func(node sdk.DAGNode) {
		nodes = append(nodes, node)
		if prevNodeID != "" {
			links = append(links, sdk.DAGLink{Source: prevNodeID, Target: node.ID, Kind: "chrono"})
		}
		prevNodeID = node.ID
	}

	for scanner.Scan() {
		lineIdx++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env dagEnv
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type != "assistant" && env.Type != "user" && env.Type != "message" {
			continue
		}
		var msg dagMsg
		if err := json.Unmarshal(env.Message, &msg); err != nil {
			continue
		}
		switch msg.Role {
		case "assistant":
			var hasToolUse bool
			turnID := fmt.Sprintf("assist-%d", lineIdx)
			for _, raw := range msg.Content {
				var b dagBlock
				if err := json.Unmarshal(raw, &b); err != nil {
					continue
				}
				if b.Type == "tool_use" && ToolNameRE.MatchString(b.Name) {
					hasToolUse = true
					nodeID := fmt.Sprintf("tool-%d-%s", lineIdx, b.ID)
					toolUseNodeID[b.ID] = nodeID
					addNode(sdk.DAGNode{
						ID:    nodeID,
						Type:  "tool",
						Label: b.Name,
						Ts:    env.Timestamp,
					})
				}
			}
			// If the turn contained only text (no tool_use), still emit an
			// assistant node so chrono edges connect across pure text turns.
			if !hasToolUse {
				addNode(sdk.DAGNode{ID: turnID, Type: "assistant", Label: "assistant", Ts: env.Timestamp})
			}
		case "user":
			// User messages may carry tool_result blocks — wire them back.
			userTurnID := fmt.Sprintf("user-%d", lineIdx)
			hadResult := false
			for _, raw := range msg.Content {
				var b dagBlock
				if err := json.Unmarshal(raw, &b); err != nil {
					continue
				}
				if b.Type == "tool_result" && b.ToolUseID != "" {
					if src, ok := toolUseNodeID[b.ToolUseID]; ok {
						resultNodeID := fmt.Sprintf("result-%d-%s", lineIdx, b.ToolUseID)
						addNode(sdk.DAGNode{
							ID:    resultNodeID,
							Type:  "user",
							Label: "tool_result",
							Ts:    env.Timestamp,
						})
						links = append(links, sdk.DAGLink{Source: src, Target: resultNodeID, Kind: "result"})
						hadResult = true
					}
				}
			}
			if !hadResult {
				addNode(sdk.DAGNode{ID: userTurnID, Type: "user", Label: "user", Ts: env.Timestamp})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return sdk.DAGData{}, err
	}
	return sdk.DAGData{Nodes: nodes, Links: links}, nil
}

// BuildCoOccurrence emits a symmetric session-count matrix. Tools are
// ordered by descending session count; ties broken alphabetically.
func BuildCoOccurrence(ctx context.Context, opts ScanOpts, dirs []string) (sdk.CoOccurrenceData, error) {
	byID, err := scanSessionsForTools(ctx, opts, dirs)
	if err != nil {
		return sdk.CoOccurrenceData{}, err
	}
	// Per-tool session set so multiple calls in one session count once.
	bySet := make(map[string]map[string]bool)
	for sessID, calls := range byID {
		for _, c := range calls {
			if _, ok := bySet[c.Name]; !ok {
				bySet[c.Name] = make(map[string]bool)
			}
			bySet[c.Name][sessID] = true
		}
	}

	type toolCount struct {
		name  string
		count int
	}
	ranked := make([]toolCount, 0, len(bySet))
	for name, sessSet := range bySet {
		ranked = append(ranked, toolCount{name: name, count: len(sessSet)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].name < ranked[j].name
	})

	truncated := false
	if len(ranked) > maxCoOccurrenceTools {
		ranked = ranked[:maxCoOccurrenceTools]
		truncated = true
	}

	tools := make([]string, len(ranked))
	for i, r := range ranked {
		tools[i] = r.name
	}
	n := len(tools)
	matrix := make([][]int, n)
	for i := range matrix {
		matrix[i] = make([]int, n)
	}
	for i, a := range tools {
		setA := bySet[a]
		for j := i; j < n; j++ {
			setB := bySet[tools[j]]
			count := 0
			// Iterate smaller set for efficiency.
			small, large := setA, setB
			if len(setB) < len(setA) {
				small, large = setB, setA
			}
			for s := range small {
				if large[s] {
					count++
				}
			}
			matrix[i][j] = count
			matrix[j][i] = count
		}
	}

	return sdk.CoOccurrenceData{
		Tools:  tools,
		Matrix: matrix,
		Meta: sdk.CoOccurrenceMeta{
			SessionCount: len(byID),
			Truncated:    truncated,
		},
	}, nil
}

// BuildSpawnTree discovers parent → subagent relationships by inspecting
// the ~/.claude/projects/{encoded}/{sessionId}/subagents/*.jsonl
// directories that the merger writes for every spawned subagent. Each
// session in the scan window becomes a node; depth is the BFS distance
// from a root (a session with no parent).
func BuildSpawnTree(ctx context.Context, opts ScanOpts, dirs []string) (sdk.SpawnTreeData, error) {
	files := DiscoverSessions(opts, dirs)
	if err := ctx.Err(); err != nil {
		return sdk.SpawnTreeData{}, err
	}

	type rec struct {
		sessionID string
		parent    string
		toolCount int
	}
	recs := make(map[string]*rec, len(files))

	for _, sf := range files {
		toolCount := 0
		// Tool count is the number of tool_use entries.
		calls, err := readToolCallsFromFile(sf.Path, sf.SessionID, opts.From, opts.To)
		if err == nil {
			toolCount = len(calls)
		}
		// Preserve any parent already recorded by an earlier iteration —
		// a sibling that processed this session's parent first would have
		// created the rec with .parent set. Overwriting unconditionally
		// would clobber that link whenever DiscoverSessions returns the
		// parent before the child (filesystem-dependent mtime ordering).
		if existing, ok := recs[sf.SessionID]; ok {
			existing.toolCount = toolCount
		} else {
			recs[sf.SessionID] = &rec{sessionID: sf.SessionID, toolCount: toolCount}
		}

		// Look for subagent directories next to this jsonl, plus any
		// nested subagents/*.jsonl that this session itself spawned.
		// Convention (per .agent-context/conventions.md):
		//   ~/.claude/projects/{encoded}/{sessionId}/subagents/*.jsonl
		// where {sessionId} matches the JSONL file basename.
		subDir := filepath.Join(filepath.Dir(sf.Path), sf.SessionID, "subagents")
		entries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			childID := strings.TrimSuffix(name, ".jsonl")
			if _, exists := recs[childID]; !exists {
				recs[childID] = &rec{sessionID: childID}
			}
			recs[childID].parent = sf.SessionID
		}
	}

	// Build node/link slices. Roots have empty parent.
	var nodes []sdk.SpawnTreeNode
	var links []sdk.SpawnTreeLink
	var roots []string
	depth := make(map[string]int, len(recs))
	// BFS from each root to compute depth.
	for id, r := range recs {
		if r.parent == "" {
			roots = append(roots, id)
			depth[id] = 0
		}
	}
	sort.Strings(roots)
	queue := append([]string{}, roots...)
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		for childID, r := range recs {
			if r.parent == head {
				depth[childID] = depth[head] + 1
				queue = append(queue, childID)
			}
		}
	}

	ids := make([]string, 0, len(recs))
	for id := range recs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := recs[id]
		nodes = append(nodes, sdk.SpawnTreeNode{
			ID:        id,
			Label:     shortLabel(id),
			Depth:     depth[id],
			ToolCount: r.toolCount,
			CostCents: 0, // Cost integration is a follow-up (see ADR-0001 / cost_history).
		})
		if r.parent != "" {
			links = append(links, sdk.SpawnTreeLink{Source: r.parent, Target: id})
		}
	}
	return sdk.SpawnTreeData{Roots: roots, Nodes: nodes, Links: links}, nil
}

// shortLabel renders an 8-char prefix of a UUID so the spawn tree shows
// a readable label without truncating client-side.
func shortLabel(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
