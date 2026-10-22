package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"agentdeck/internal/agent"
	"agentdeck/internal/store"
)

type streamCommand struct {
	request Request
	hash    [sha256.Size]byte
}

// Native commands can wait on an agent for seconds. Keep them off the socket
// reader/writer loop so history, live events and heartbeats remain available.
// One worker per stream preserves command order; commandMu retains cross-stream
// deduplication. Only the stream loop encrypts and writes responses.
func (s *Server) runStreamCommands(owner string, commands <-chan streamCommand, results chan<- map[string]any, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case command := <-commands:
			response := s.streamCommand(owner, command, stop)
			if response == nil {
				return
			}
			select {
			case results <- response:
			case <-stop:
				return
			}
		}
	}
}

func (s *Server) streamCommand(owner string, command streamCommand, stop <-chan struct{}) map[string]any {
	req := command.request
	failure := func(err error, notSent bool) map[string]any {
		return map[string]any{"type": "error", "id": req.ID, "message": err.Error(), "not_sent": notSent}
	}
	if s.Manager == nil || req.ID == "" || len(req.ID) > 100 {
		return failure(errors.New("工作站未启用项目操作"), true)
	}
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	// A disconnected stream must not start more native work after waiting for
	// another command. Already-started work records its outcome for safe retries.
	select {
	case <-stop:
		return nil
	default:
	}
	var device store.Device
	if s.Store.Get("devices", owner, &device) != nil {
		return failure(errors.New("设备配对已被工作站删除，请重新配对"), true)
	}
	commandKey := owner + "/" + req.ID
	hash := hex.EncodeToString(command.hash[:])
	var previous struct {
		Result  any
		Error   string
		Hash    string
		NotSent bool
	}
	if s.Store.Get("commands", commandKey, &previous) == nil {
		if previous.Hash != hash {
			return failure(errors.New("请求标识已用于不同操作"), true)
		}
		if previous.Error != "" {
			return failure(errors.New(previous.Error), previous.NotSent)
		}
		return map[string]any{"type": "result", "id": req.ID, "result": previous.Result}
	}
	if s.updateDraining {
		return failure(errors.New("工作站正在更新，请稍后重试"), true)
	}
	previous.Hash = hash
	previous.Error = "操作结果尚未确认，请检查会话后再发送"
	if err := s.Store.Put("commands", commandKey, previous); err != nil {
		return failure(errors.New("操作回执保存失败，请刷新核对，勿重复发送"), true)
	}
	var result any
	var err error
	switch req.Method {
	case "session_permissions_change":
		result, err = s.Manager.ChangePermissions(req.Session, req.PermissionVersion, req.PermissionMode)
	case "goal_change":
		err = s.Manager.ChangeGoal(req.Session, req.GoalVersion, req.GoalAction, req.ID)
	case "create":
		result, err = s.Manager.CreatePermission(req.Agent, req.Project, req.Session, req.PermissionMode)
	case "send":
		var files []agent.InputAttachment
		files, err = s.inputFiles(owner, req.Session, req.Attachments)
		if err != nil {
			err = &agent.InputNotSentError{Err: err}
		}
		if err == nil {
			err = s.Manager.SendDelivery(req.Session, req.Text, req.ID, agent.TurnOptions{Model: req.Model, Effort: req.Effort, Speed: req.Speed}, files, req.Delivery)
		}
		if err == nil {
			result = s.Manager.NativeSubmission(req.Session, req.ID)
			_ = s.markUploadsUsed(owner, req.Session, req.Attachments)
		}
	case "queue_edit", "queue_steer", "queue_delete":
		err = s.Manager.ChangeQueue(req.Session, req.QueueID, req.QueueVersion, strings.TrimPrefix(req.Method, "queue_"), req.Text, req.ID)
	case "queue_cancel":
		err = s.Manager.CancelDelivery(req.Session, req.QueueID)
	case "interrupt":
		err = s.Manager.Interrupt(req.Session)
	case "approve":
		err = s.Manager.Approve(req.Session, req.Approval, req.Decision, req.Text)
	}
	previous.Result = result
	previous.Error = ""
	if err != nil {
		previous.Error = err.Error()
		previous.NotSent = agent.InputDefinitelyNotSent(err)
	}
	if saveErr := s.Store.Put("commands", commandKey, previous); saveErr != nil {
		return failure(errors.New("操作结果尚未确认，请检查会话后再发送"), false)
	}
	if err != nil {
		return failure(err, previous.NotSent)
	}
	return map[string]any{"type": "result", "id": req.ID, "result": result}
}
