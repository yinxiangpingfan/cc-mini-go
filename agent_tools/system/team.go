package system

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/fileops"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
)

const (
	teamNameDefault  = "default"
	teamLeadName     = "lead"
	teamMaxTurns     = 50
	teamJitterFactor = 0.25
)

var (
	teamMaxLLMRetries  = 10
	teamBaseRetryDelay = 500 * time.Millisecond
	teamMaxRetryDelay  = 32 * time.Second
)

type TeamStatus string

const (
	TeamIdle     TeamStatus = "idle"
	TeamWorking  TeamStatus = "working"
	TeamShutdown TeamStatus = "shutdown"
)

var validTeamMessageTypes = map[string]bool{
	"message":                true,
	"broadcast":              true,
	"shutdown_request":       true,
	"shutdown_response":      true,
	"plan_approval":          true,
	"plan_approval_response": true,
}

type TeamMember struct {
	Name   string     `json:"name"`
	Role   string     `json:"role"`
	Status TeamStatus `json:"status"`
}

type TeamConfig struct {
	TeamName string       `json:"team_name"`
	Members  []TeamMember `json:"members"`
}

type MessageEnvelope struct {
	Type      string  `json:"type"`
	From      string  `json:"from"`
	To        string  `json:"to,omitempty"`
	Content   string  `json:"content"`
	Timestamp float64 `json:"timestamp"`
}

type MessageBus struct {
	dir string
	mu  sync.Mutex
}

func NewMessageBus(dir string) *MessageBus {
	if dir == "" {
		dir = shared.TeamInboxDir()
	}
	return &MessageBus{dir: dir}
}

func validMailboxName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return false
	}
	return filepath.Base(name) == name && !strings.ContainsAny(name, `/\`)
}

func (b *MessageBus) inboxPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !validMailboxName(name) {
		return "", fmt.Errorf(errors.ErrToolFunctionCall, "name")
	}
	return filepath.Join(b.dir, name+".jsonl"), nil
}

func normalizeTeamMessageType(msgType string) (string, error) {
	msgType = strings.TrimSpace(msgType)
	if msgType == "" {
		msgType = "message"
	}
	if !validTeamMessageTypes[msgType] {
		return "", fmt.Errorf("invalid team message type %q", msgType)
	}
	return msgType, nil
}

func (b *MessageBus) Send(sender, to, content, msgType string) (MessageEnvelope, error) {
	sender = strings.TrimSpace(sender)
	to = strings.TrimSpace(to)
	content = strings.TrimSpace(content)
	if !validMailboxName(sender) {
		return MessageEnvelope{}, fmt.Errorf(errors.ErrToolFunctionCall, "from")
	}
	if !validMailboxName(to) {
		return MessageEnvelope{}, fmt.Errorf(errors.ErrToolFunctionCall, "to")
	}
	if content == "" {
		return MessageEnvelope{}, fmt.Errorf(errors.ErrToolFunctionCall, "content")
	}
	normalizedType, err := normalizeTeamMessageType(msgType)
	if err != nil {
		return MessageEnvelope{}, err
	}
	env := MessageEnvelope{
		Type:      normalizedType,
		From:      sender,
		To:        to,
		Content:   content,
		Timestamp: unixSeconds(time.Now()),
	}
	line, _ := json.Marshal(env)

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return MessageEnvelope{}, err
	}
	path, err := b.inboxPath(to)
	if err != nil {
		return MessageEnvelope{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return MessageEnvelope{}, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return MessageEnvelope{}, err
	}
	return env, nil
}

func (b *MessageBus) ReadInbox(name string) ([]MessageEnvelope, error) {
	path, err := b.inboxPath(name)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	messages := make([]MessageEnvelope, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var env MessageEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		messages = append(messages, env)
	}
	return messages, nil
}

func (b *MessageBus) HasMessages(name string) bool {
	path, err := b.inboxPath(name)
	if err != nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func (b *MessageBus) Broadcast(sender, content string, recipients []string) (int, error) {
	count := 0
	for _, to := range recipients {
		if to == sender {
			continue
		}
		if _, err := b.Send(sender, to, content, "broadcast"); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

type teammateWorker struct {
	name     string
	role     string
	messages []any
	running  bool
}

type teammateTurnFunc func(ctx context.Context, name, role string, messages []any) ([]any, bool, string, error)

type TeammateManager struct {
	dir        string
	configPath string
	config     TeamConfig
	bus        *MessageBus
	call       *client.Call
	model      string
	workers    map[string]*teammateWorker
	runTurn    teammateTurnFunc
	mu         sync.Mutex
}

func NewTeammateManager(call *client.Call, model string) *TeammateManager {
	m := &TeammateManager{
		dir:        shared.TeamDir(),
		configPath: shared.TeamConfigFile(),
		bus:        NewMessageBus(shared.TeamInboxDir()),
		call:       call,
		model:      model,
		workers:    make(map[string]*teammateWorker),
	}
	m.runTurn = m.defaultRunTurn
	return m
}

func (m *TeammateManager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadConfigLocked()
	for i := range m.config.Members {
		if m.config.Members[i].Status == TeamWorking {
			m.config.Members[i].Status = TeamIdle
		}
	}
	_ = m.saveConfigLocked()
}

func (m *TeammateManager) loadConfigLocked() {
	m.config = TeamConfig{TeamName: teamNameDefault, Members: []TeamMember{}}
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return
	}
	var cfg TeamConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	if cfg.TeamName == "" {
		cfg.TeamName = teamNameDefault
	}
	if cfg.Members == nil {
		cfg.Members = []TeamMember{}
	}
	m.config = cfg
}

func (m *TeammateManager) saveConfigLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o755); err != nil {
		return err
	}
	if m.config.TeamName == "" {
		m.config.TeamName = teamNameDefault
	}
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.configPath, append(data, '\n'), 0o644)
}

func (m *TeammateManager) memberIndexLocked(name string) int {
	for i := range m.config.Members {
		if m.config.Members[i].Name == name {
			return i
		}
	}
	return -1
}

func (m *TeammateManager) memberNamesLocked() []string {
	names := make([]string, 0, len(m.config.Members))
	for _, member := range m.config.Members {
		if member.Status != TeamShutdown {
			names = append(names, member.Name)
		}
	}
	return names
}

func (m *TeammateManager) ensureWorkerLocked(name, role string) *teammateWorker {
	w := m.workers[name]
	if w == nil {
		w = &teammateWorker{name: name, role: role}
		m.workers[name] = w
	}
	if role != "" {
		w.role = role
	}
	return w
}

func (m *TeammateManager) setStatusLocked(name string, status TeamStatus) {
	if idx := m.memberIndexLocked(name); idx >= 0 {
		m.config.Members[idx].Status = status
	}
}

func (m *TeammateManager) Spawn(name, role, taskPrompt string) (string, error) {
	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	taskPrompt = strings.TrimSpace(taskPrompt)
	if !validMailboxName(name) || name == teamLeadName {
		return "", fmt.Errorf(errors.ErrToolFunctionCall, "name")
	}
	if role == "" {
		return "", fmt.Errorf(errors.ErrToolFunctionCall, "role")
	}
	if taskPrompt == "" {
		return "", fmt.Errorf(errors.ErrToolFunctionCall, "prompt")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadConfigLocked()
	idx := m.memberIndexLocked(name)
	if idx >= 0 {
		member := &m.config.Members[idx]
		if member.Status == TeamWorking {
			return "", fmt.Errorf("teammate %q is currently working", name)
		}
		if member.Status == TeamShutdown {
			return "", fmt.Errorf("teammate %q is shutdown", name)
		}
		member.Role = role
		member.Status = TeamIdle
	} else {
		m.config.Members = append(m.config.Members, TeamMember{Name: name, Role: role, Status: TeamIdle})
	}
	w := m.ensureWorkerLocked(name, role)
	w.messages = append(w.messages, *client.NewChatCompletionMessage().NewUserMessage(taskPrompt))
	if err := m.saveConfigLocked(); err != nil {
		return "", err
	}
	m.wakeLocked(name)
	return fmt.Sprintf("Spawned teammate %q (role: %s)", name, role), nil
}

func (m *TeammateManager) Send(sender, to, content, msgType string) (string, error) {
	sender = strings.TrimSpace(sender)
	if sender == "" {
		sender = teamLeadName
	}
	to = strings.TrimSpace(to)
	if to != teamLeadName {
		m.mu.Lock()
		m.loadConfigLocked()
		idx := m.memberIndexLocked(to)
		if idx < 0 {
			m.mu.Unlock()
			return "", fmt.Errorf("teammate %q not found", to)
		}
		if m.config.Members[idx].Status == TeamShutdown {
			m.mu.Unlock()
			return "", fmt.Errorf("teammate %q is shutdown", to)
		}
		m.mu.Unlock()
	}
	env, err := m.bus.Send(sender, to, content, msgType)
	if err != nil {
		return "", err
	}
	if to != teamLeadName {
		m.mu.Lock()
		m.loadConfigLocked()
		idx := m.memberIndexLocked(to)
		if idx < 0 {
			m.mu.Unlock()
			return "", fmt.Errorf("teammate %q not found", to)
		}
		m.ensureWorkerLocked(to, m.config.Members[idx].Role)
		m.wakeLocked(to)
		m.mu.Unlock()
	}
	data, _ := json.Marshal(map[string]any{"sent": true, "type": env.Type, "from": env.From, "to": env.To})
	return string(data), nil
}

func (m *TeammateManager) Broadcast(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf(errors.ErrToolFunctionCall, "content")
	}
	m.mu.Lock()
	m.loadConfigLocked()
	names := m.memberNamesLocked()
	m.mu.Unlock()
	count, err := m.bus.Broadcast(teamLeadName, content, names)
	if err != nil {
		return "", err
	}
	for _, name := range names {
		m.mu.Lock()
		m.wakeLocked(name)
		m.mu.Unlock()
	}
	return fmt.Sprintf("Broadcast to %d teammates", count), nil
}

func (m *TeammateManager) ReadLeadInbox() (string, error) {
	messages, err := m.bus.ReadInbox(teamLeadName)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(messages, "", "  ")
	return string(data), nil
}

func (m *TeammateManager) DrainLeadInbox() []MessageEnvelope {
	messages, err := m.bus.ReadInbox(teamLeadName)
	if err != nil {
		return nil
	}
	return messages
}

func (m *TeammateManager) ListAll() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadConfigLocked()
	if len(m.config.Members) == 0 {
		return "No teammates."
	}
	members := append([]TeamMember(nil), m.config.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	lines := []string{fmt.Sprintf("Team: %s", m.config.TeamName)}
	for _, member := range members {
		lines = append(lines, fmt.Sprintf("  %s (%s): %s", member.Name, member.Role, member.Status))
	}
	return strings.Join(lines, "\n")
}

func (m *TeammateManager) wakeLocked(name string) {
	idx := m.memberIndexLocked(name)
	if idx < 0 || m.config.Members[idx].Status == TeamShutdown {
		return
	}
	w := m.ensureWorkerLocked(name, m.config.Members[idx].Role)
	if w.running {
		return
	}
	w.running = true
	m.config.Members[idx].Status = TeamWorking
	_ = m.saveConfigLocked()
	go m.runWorker(name)
}

func (m *TeammateManager) runWorker(name string) {
	for turn := 0; turn < teamMaxTurns; turn++ {
		m.mu.Lock()
		w := m.workers[name]
		if w == nil {
			m.mu.Unlock()
			return
		}
		role := w.role
		messages := append([]any(nil), w.messages...)
		m.mu.Unlock()

		inbox, err := m.bus.ReadInbox(name)
		if err != nil {
			_, _ = m.bus.Send(name, teamLeadName, shared.JsonErr(err.Error()), "message")
			break
		}
		for _, msg := range inbox {
			data, _ := json.Marshal(msg)
			messages = append(messages, *client.NewChatCompletionMessage().NewUserMessage(string(data)))
		}
		if len(messages) == 0 {
			break
		}

		next, keepGoing, finalText, err := m.runTurn(context.Background(), name, role, messages)
		m.mu.Lock()
		if w := m.workers[name]; w != nil {
			w.messages = append([]any(nil), next...)
		}
		m.mu.Unlock()
		if err != nil {
			_, _ = m.bus.Send(name, teamLeadName, shared.JsonErr(err.Error()), "message")
			break
		}
		if !keepGoing {
			if strings.TrimSpace(finalText) != "" {
				_, _ = m.bus.Send(name, teamLeadName, finalText, "message")
			}
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.workers, name)
	if idx := m.memberIndexLocked(name); idx >= 0 && m.config.Members[idx].Status != TeamShutdown {
		m.config.Members[idx].Status = TeamIdle
	}
	_ = m.saveConfigLocked()
	if m.bus.HasMessages(name) {
		m.wakeLocked(name)
	}
}

func teamIsRetryableStatusCode(code int) bool {
	switch code {
	case 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func teamParseRetryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func teamBackoffDelay(attempt int) time.Duration {
	d := teamBaseRetryDelay << (attempt - 1)
	if d <= 0 || d > teamMaxRetryDelay {
		d = teamMaxRetryDelay
	}
	jitter := time.Duration(rand.Int64N(int64(float64(d)*teamJitterFactor) + 1))
	return d + jitter
}

func teamComputeRetryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return teamBackoffDelay(attempt)
}

func (m *TeammateManager) callWithRetry(ctx context.Context, messages []any, systemPrompt string, tools []client.Tool) (client.CallResponse, *http.Response, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt <= teamMaxLLMRetries; attempt++ {
		if attempt > 0 {
			delay := teamComputeRetryDelay(attempt, retryAfter)
			select {
			case <-ctx.Done():
				return client.CallResponse{}, nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		retryAfter = 0
		res, resp, err := m.call.NewCallRequestCtx(ctx, m.model, messages, false, systemPrompt, tools, nil)
		if resp != nil && resp.StatusCode != http.StatusOK {
			statusErr := fmt.Errorf(errors.ErrHTTPStatusCode, resp.StatusCode)
			if err != nil {
				statusErr = fmt.Errorf("%w: %v", statusErr, err)
			}
			if !teamIsRetryableStatusCode(resp.StatusCode) {
				return client.CallResponse{}, resp, statusErr
			}
			retryAfter = teamParseRetryAfter(resp.Header)
			lastErr = statusErr
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return client.CallResponse{}, resp, ctx.Err()
			}
			lastErr = err
			continue
		}
		return res, resp, nil
	}
	return client.CallResponse{}, nil, fmt.Errorf("teammate LLM request exhausted retries (%d retries): %w", teamMaxLLMRetries, lastErr)
}

func (m *TeammateManager) defaultRunTurn(ctx context.Context, name, role string, messages []any) ([]any, bool, string, error) {
	if m.call == nil {
		return messages, false, "", fmt.Errorf("team manager has no client")
	}
	handlers := make(map[string]shared.ToolFunc)
	tools := m.teammateTools(name, &handlers)
	systemPrompt := fmt.Sprintf("You are %q, role: %s. Work inside this project. Use send_message to communicate with lead or teammates. Complete assigned work, then summarize briefly.", name, role)
	res, resp, err := m.callWithRetry(ctx, messages, systemPrompt, tools)
	if resp != nil && resp.StatusCode != 200 {
		msg := fmt.Sprintf(errors.ErrHTTPStatusCode, resp.StatusCode)
		if err != nil {
			msg = fmt.Sprintf("%s: %v", msg, err)
		}
		return messages, false, "", fmt.Errorf("%s", msg)
	}
	if err != nil {
		return messages, false, "", err
	}
	if len(res.Choices) == 0 {
		return messages, false, "", nil
	}
	choice := res.Choices[0]
	if len(choice.Message.ToolCalls) == 0 {
		finalText, _ := choice.Message.Content.(string)
		if finalText != "" {
			messages = append(messages, *m.call.Cm.NewAssistantMessage(finalText))
		}
		return messages, false, finalText, nil
	}

	messages = append(messages, *m.call.Cm.NewToolsCall(choice.Message.Content, choice.Message.ToolCalls))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, tc := range choice.Message.ToolCalls {
		tc := tc
		handler, ok := handlers[tc.Function.Name]
		if !ok {
			messages = append(messages, *m.call.Cm.NewToolsMessage(tc.Id, shared.JsonErr("unknown teammate tool: "+tc.Function.Name)))
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			result := handler(ctx, args)
			mu.Lock()
			messages = append(messages, *m.call.Cm.NewToolsMessage(tc.Id, result))
			mu.Unlock()
		}()
	}
	wg.Wait()
	return messages, true, "", nil
}

func (m *TeammateManager) teammateTools(sender string, handlers *map[string]shared.ToolFunc) []client.Tool {
	bash := NewBashTool()
	(*handlers)[bash.Name] = bash.Func
	readFile := fileops.NewReadFile()
	(*handlers)[readFile.Name] = readFile.Func
	writeFile := fileops.NewWriteFileTool()
	(*handlers)[writeFile.Name] = writeFile.Func
	editFile := fileops.NewEditFileTool()
	(*handlers)[editFile.Name] = editFile.Func

	send := &Tools{
		Name: "send_message",
		Func: func(ctx context.Context, args map[string]any) string {
			to, ok := args["to"].(string)
			if !ok || strings.TrimSpace(to) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "to"))
			}
			content, ok := args["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "content"))
			}
			msgType, _ := args["msg_type"].(string)
			result, err := m.Send(sender, to, content, msgType)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
	(*handlers)[send.Name] = send.Func

	readInbox := &Tools{
		Name: "read_inbox",
		Func: func(ctx context.Context, args map[string]any) string {
			messages, err := m.bus.ReadInbox(sender)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			data, _ := json.MarshalIndent(messages, "", "  ")
			return string(data)
		},
	}
	(*handlers)[readInbox.Name] = readInbox.Func

	return []client.Tool{
		bash.BashToolForLLM(),
		readFile.ReadFileInfoForLLm(),
		writeFile.WriteFileInfoForLLm(),
		editFile.EditFileInfoForLLm(),
		send.SendMessageInfoForLLM(),
		readInbox.ReadInboxInfoForLLM(),
	}
}

func FormatTeamInbox(messages []MessageEnvelope) string {
	if len(messages) == 0 {
		return ""
	}
	data, _ := json.MarshalIndent(messages, "", "  ")
	return "<team-inbox>\n" + string(data) + "\n</team-inbox>"
}

func NewSpawnTeammateTool(mgr *TeammateManager) *Tools {
	return &Tools{
		Name: "spawn_teammate",
		Func: func(ctx context.Context, args map[string]any) string {
			name, ok := args["name"].(string)
			if !ok || strings.TrimSpace(name) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "name"))
			}
			role, ok := args["role"].(string)
			if !ok || strings.TrimSpace(role) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "role"))
			}
			taskPrompt, ok := args["prompt"].(string)
			if !ok || strings.TrimSpace(taskPrompt) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "prompt"))
			}
			result, err := mgr.Spawn(name, role, taskPrompt)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

func NewListTeammatesTool(mgr *TeammateManager) *Tools {
	return &Tools{Name: "list_teammates", Func: func(ctx context.Context, args map[string]any) string { return mgr.ListAll() }}
}

func NewSendMessageTool(mgr *TeammateManager) *Tools {
	return &Tools{
		Name: "send_message",
		Func: func(ctx context.Context, args map[string]any) string {
			to, ok := args["to"].(string)
			if !ok || strings.TrimSpace(to) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "to"))
			}
			content, ok := args["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "content"))
			}
			msgType, _ := args["msg_type"].(string)
			result, err := mgr.Send(teamLeadName, to, content, msgType)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

func NewReadTeamInboxTool(mgr *TeammateManager) *Tools {
	return &Tools{
		Name: "read_inbox",
		Func: func(ctx context.Context, args map[string]any) string {
			result, err := mgr.ReadLeadInbox()
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

func NewBroadcastTool(mgr *TeammateManager) *Tools {
	return &Tools{
		Name: "broadcast",
		Func: func(ctx context.Context, args map[string]any) string {
			content, ok := args["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "content"))
			}
			result, err := mgr.Broadcast(content)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

func (t *Tools) SpawnTeammateInfoForLLM() client.Tool {
	return client.Tool{Type: "function", Function: client.FunctionDefinition{Name: "spawn_teammate", Description: "Spawn a persistent named teammate with its own context and inbox.", Parameters: client.FunctionParameters{Type: "object", Properties: map[string]any{"name": client.ParameterProperty{Type: "string", Description: "Teammate name, e.g. alice."}, "role": client.ParameterProperty{Type: "string", Description: "Teammate role, e.g. coder or tester."}, "prompt": client.ParameterProperty{Type: "string", Description: "Initial assignment for the teammate."}}, Required: []string{"name", "role", "prompt"}}}}
}

func (t *Tools) ListTeammatesInfoForLLM() client.Tool {
	return client.Tool{Type: "function", Function: client.FunctionDefinition{Name: "list_teammates", Description: "List persistent teammates with name, role, and status.", Parameters: client.FunctionParameters{Type: "object", Properties: map[string]any{}}}}
}

func (t *Tools) SendMessageInfoForLLM() client.Tool {
	return client.Tool{Type: "function", Function: client.FunctionDefinition{Name: "send_message", Description: "Send a message to a teammate inbox.", Parameters: client.FunctionParameters{Type: "object", Properties: map[string]any{"to": client.ParameterProperty{Type: "string", Description: "Recipient teammate name, or lead."}, "content": client.ParameterProperty{Type: "string", Description: "Message content."}, "msg_type": client.ParameterProperty{Type: "string", Description: "Message type.", Enum: []string{"message", "broadcast", "shutdown_request", "shutdown_response", "plan_approval", "plan_approval_response"}}}, Required: []string{"to", "content"}}}}
}

func (t *Tools) ReadInboxInfoForLLM() client.Tool {
	return client.Tool{Type: "function", Function: client.FunctionDefinition{Name: "read_inbox", Description: "Read and drain your inbox.", Parameters: client.FunctionParameters{Type: "object", Properties: map[string]any{}}}}
}

func (t *Tools) BroadcastInfoForLLM() client.Tool {
	return client.Tool{Type: "function", Function: client.FunctionDefinition{Name: "broadcast", Description: "Broadcast a message to all active teammates.", Parameters: client.FunctionParameters{Type: "object", Properties: map[string]any{"content": client.ParameterProperty{Type: "string", Description: "Message content to broadcast."}}, Required: []string{"content"}}}}
}
