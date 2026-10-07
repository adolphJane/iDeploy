package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type ControlOperationView struct {
	ID        string                      `json:"id"`
	Command   string                      `json:"command"`
	Status    string                      `json:"status"`
	Result    *ControlOperationResultView `json:"result,omitempty"`
	CreatedAt string                      `json:"createdAt"`
	UpdatedAt string                      `json:"updatedAt"`
}

type ControlOperationResultView struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

var controlRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{7,127}$`)

func executeIdempotentCLIRequest(ctx context.Context, service *SystemService, request cliRequest) cliResponse {
	request.RequestID = strings.TrimSpace(request.RequestID)
	if request.RequestID == "" || request.Command == "operation.get" || service == nil {
		return executeCLIRequest(ctx, service, request)
	}
	if !controlRequestIDPattern.MatchString(request.RequestID) {
		return errorCLIResponse("invalid_request", "request_id must be 8 to 128 safe characters")
	}
	hash, err := controlRequestHash(request)
	if err != nil {
		return errorCLIResponse("encoding_failed", err.Error())
	}
	operation, claimed, err := service.claimControlOperation(ctx, request.RequestID, request.Command, hash)
	if err != nil {
		return errorCLIResponse("operation_failed", err.Error())
	}
	if !claimed {
		if operation.Command != request.Command || operation.requestHash != hash {
			return errorCLIResponse("idempotency_conflict", "request_id was already used for a different request")
		}
		if operation.response != nil {
			return *operation.response
		}
		response := errorCLIResponse("operation_pending", "request was accepted and is still being processed")
		response.Data, _ = json.Marshal(operation.public())
		return response
	}

	response := executeCLIRequest(ctx, service, request)
	if err := service.finishControlOperation(context.Background(), request.RequestID, response); err != nil {
		return errorCLIResponse("operation_failed", "operation finished but its result could not be persisted: "+err.Error())
	}
	return response
}

func controlRequestHash(request cliRequest) (string, error) {
	request.RequestID = ""
	request.TimeoutMS = 0
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type storedControlOperation struct {
	ControlOperationView
	requestHash string
	response    *cliResponse
}

func (value storedControlOperation) public() ControlOperationView {
	view := value.ControlOperationView
	if value.response != nil {
		result := ControlOperationResultView{OK: value.response.OK, Error: value.response.Error, Code: value.response.Code}
		if len(value.response.Data) > 0 {
			_ = json.Unmarshal(value.response.Data, &result.Data)
		}
		view.Result = &result
	}
	return view
}

func (s *SystemService) claimControlOperation(ctx context.Context, id, command, hash string) (storedControlOperation, bool, error) {
	db, err := s.systemDatabase()
	if err != nil {
		return storedControlOperation{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO control_operations(id,command,request_hash,status,created_at,updated_at)
		VALUES (?,?,?,'running',?,?)
	`, id, command, hash, now, now)
	if err != nil {
		return storedControlOperation{}, false, fmt.Errorf("create control operation: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storedControlOperation{}, false, err
	}
	value, err := s.loadControlOperation(ctx, id)
	return value, affected == 1, err
}

func (s *SystemService) finishControlOperation(ctx context.Context, id string, response cliResponse) error {
	db, err := s.systemDatabase()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	status := "succeeded"
	if !response.OK {
		status = "failed"
	}
	finishContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := db.ExecContext(finishContext, `
		UPDATE control_operations SET status=?, response_json=?, updated_at=? WHERE id=?
	`, status, string(encoded), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("finish control operation: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return err
		}
		return errors.New("control operation disappeared")
	}
	return nil
}

func recoverInterruptedControlOperations(ctx context.Context, db *sql.DB) error {
	response, err := json.Marshal(errorCLIResponse("operation_interrupted", "iDeploy restarted before the configuration operation completed"))
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE control_operations SET status='failed', response_json=?, updated_at=? WHERE status='running'
	`, string(response), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("recover control operations: %w", err)
	}
	return nil
}

func (s *SystemService) GetControlOperation(id string) (ControlOperationView, error) {
	id = strings.TrimSpace(id)
	if !controlRequestIDPattern.MatchString(id) {
		return ControlOperationView{}, errors.New("operation_id is invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	value, err := s.loadControlOperation(ctx, id)
	if err != nil {
		return ControlOperationView{}, err
	}
	return value.public(), nil
}

func (s *SystemService) loadControlOperation(ctx context.Context, id string) (storedControlOperation, error) {
	db, err := s.systemDatabase()
	if err != nil {
		return storedControlOperation{}, err
	}
	var value storedControlOperation
	var responseJSON string
	err = db.QueryRowContext(ctx, `
		SELECT id,command,request_hash,status,response_json,created_at,updated_at
		FROM control_operations WHERE id=?
	`, id).Scan(&value.ID, &value.Command, &value.requestHash, &value.Status, &responseJSON, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storedControlOperation{}, &cliCommandError{Code: "not_found", Message: "operation not found"}
	}
	if err != nil {
		return storedControlOperation{}, fmt.Errorf("load control operation: %w", err)
	}
	if responseJSON != "" {
		var response cliResponse
		if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
			return storedControlOperation{}, fmt.Errorf("decode control operation result: %w", err)
		}
		value.response = &response
	}
	return value, nil
}
