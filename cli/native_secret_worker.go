package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	nativeSecretWorkerCommand  = "internal-native-secret-worker"
	nativeSecretWorkerSchema   = 1
	nativeSecretWorkerMaxInput = 8192
)

type nativeSecretOperation string

const (
	nativeSecretGet         nativeSecretOperation = "get"
	nativeSecretSet         nativeSecretOperation = "set"
	nativeSecretDelete      nativeSecretOperation = "delete"
	nativeSecretDeleteExact nativeSecretOperation = "delete_exact"
)

type nativeSecretWorkerRequest struct {
	SchemaVersion int                   `json:"schema_version"`
	Operation     nativeSecretOperation `json:"operation"`
	UI            SecretStoreUIPolicy   `json:"ui"`
	Service       string                `json:"service"`
	Account       string                `json:"account"`
	Value         []byte                `json:"value,omitempty"`
}

type nativeSecretWorkerResponse struct {
	SchemaVersion int            `json:"schema_version"`
	Found         bool           `json:"found,omitempty"`
	Value         []byte         `json:"value,omitempty"`
	ErrorCode     CloudErrorCode `json:"error_code,omitempty"`
}

var nativeSecretWorkerParentVerified = platformNativeSecretWorkerParentVerified
var nativeSecretWorkerCommandForProcess = newNativeSecretWorkerCommand

func runNativeSecretWorkerProcess(
	ctx context.Context,
	request nativeSecretWorkerRequest,
) (nativeSecretWorkerResponse, error) {
	if err := validateNativeSecretWorkerRequest(request); err != nil {
		return nativeSecretWorkerResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return nativeSecretWorkerResponse{}, nativeSecretWorkerTimeout(
			request.Operation,
			err,
		)
	}
	executable, err := os.Executable()
	if err != nil {
		return nativeSecretWorkerResponse{}, newCloudError(
			CloudErrSecretStore,
			"locate native secure-storage worker",
			err,
		)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nativeSecretWorkerResponse{}, newCloudError(
			CloudErrSecretStore,
			"encode native secure-storage request",
			err,
		)
	}
	defer zeroSecretBytes(payload)
	if len(payload) > nativeSecretWorkerMaxInput {
		return nativeSecretWorkerResponse{}, newCloudError(
			CloudErrInvalidInput,
			"encode native secure-storage request",
			nil,
		)
	}

	command := nativeSecretWorkerCommandForProcess(ctx, executable)
	command.Stdin = bytes.NewReader(payload)
	output := newCappedSecretOutput(nativeSecretWorkerMaxInput)
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		zeroSecretBytes(output.Bytes())
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nativeSecretWorkerResponse{}, nativeSecretWorkerTimeout(
				request.Operation,
				ctxErr,
			)
		}
		if command.ProcessState != nil {
			return nativeSecretWorkerResponse{}, nativeSecretWorkerFailure(
				request.Operation,
				err,
			)
		}
		return nativeSecretWorkerResponse{}, newCloudError(
			CloudErrSecretStore,
			"start native secure-storage worker",
			err,
		)
	}
	if output.Overflowed() {
		zeroSecretBytes(output.Bytes())
		return nativeSecretWorkerResponse{}, nativeSecretWorkerFailure(
			request.Operation,
			errors.New("native secure-storage response exceeds the limit"),
		)
	}

	var response nativeSecretWorkerResponse
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		zeroSecretBytes(output.Bytes())
		return nativeSecretWorkerResponse{}, nativeSecretWorkerFailure(
			request.Operation,
			err,
		)
	}
	if err := requireJSONEOF(decoder); err != nil {
		zeroSecretBytes(response.Value)
		zeroSecretBytes(output.Bytes())
		return nativeSecretWorkerResponse{}, nativeSecretWorkerFailure(
			request.Operation,
			err,
		)
	}
	zeroSecretBytes(output.Bytes())
	if err := validateNativeSecretWorkerResponse(request.Operation, response); err != nil {
		zeroSecretBytes(response.Value)
		return nativeSecretWorkerResponse{}, nativeSecretWorkerFailure(
			request.Operation,
			err,
		)
	}
	if response.ErrorCode != "" {
		err := nativeSecretWorkerError(
			response.ErrorCode,
		)
		if IsCloudErrorCode(err, CloudErrSecretStore) &&
			response.ErrorCode != CloudErrSecretStore {
			return nativeSecretWorkerResponse{}, nativeSecretWorkerFailure(
				request.Operation,
				err,
			)
		}
		return nativeSecretWorkerResponse{}, err
	}
	return response, nil
}

func newNativeSecretWorkerCommand(
	ctx context.Context,
	executable string,
) *exec.Cmd {
	return exec.CommandContext(ctx, executable, nativeSecretWorkerCommand)
}

func maybeRunNativeSecretWorker(
	args []string,
	input io.Reader,
	output io.Writer,
) (bool, int) {
	if len(args) != 1 || args[0] != nativeSecretWorkerCommand {
		return false, 0
	}
	if !nativeSecretWorkerParentVerified() {
		return true, 1
	}
	requestBytes, err := io.ReadAll(io.LimitReader(
		input,
		nativeSecretWorkerMaxInput+1,
	))
	if err != nil || len(requestBytes) > nativeSecretWorkerMaxInput {
		zeroSecretBytes(requestBytes)
		return true, 1
	}
	defer zeroSecretBytes(requestBytes)
	var request nativeSecretWorkerRequest
	decoder := json.NewDecoder(bytes.NewReader(requestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return true, 1
	}
	if err := requireJSONEOF(decoder); err != nil {
		zeroSecretBytes(request.Value)
		return true, 1
	}
	if err := validateNativeSecretWorkerRequest(request); err != nil {
		zeroSecretBytes(request.Value)
		return true, 1
	}
	response := platformRunNativeSecretWorker(request)
	zeroSecretBytes(request.Value)
	if response.SchemaVersion == 0 {
		response.SchemaVersion = nativeSecretWorkerSchema
	}
	if err := validateNativeSecretWorkerResponse(request.Operation, response); err != nil {
		zeroSecretBytes(response.Value)
		response = nativeSecretWorkerResponse{
			SchemaVersion: nativeSecretWorkerSchema,
			ErrorCode:     nativeSecretWorkerResponseErrorCode(request.Operation),
		}
	}
	if err := json.NewEncoder(output).Encode(response); err != nil {
		zeroSecretBytes(response.Value)
		return true, 1
	}
	zeroSecretBytes(response.Value)
	return true, 0
}

func validateNativeSecretWorkerRequest(request nativeSecretWorkerRequest) error {
	if request.SchemaVersion != nativeSecretWorkerSchema {
		return invalidNativeSecretWorkerRequest()
	}
	if err := validateSecretUIPolicy(request.UI); err != nil {
		return invalidNativeSecretWorkerRequest()
	}
	if !validNativeSecretWorkerKey(request.Service, request.Account) {
		return invalidNativeSecretWorkerRequest()
	}
	switch request.Operation {
	case nativeSecretGet, nativeSecretDelete:
		if len(request.Value) != 0 {
			return invalidNativeSecretWorkerRequest()
		}
	case nativeSecretSet, nativeSecretDeleteExact:
		if len(request.Value) == 0 ||
			len(request.Value) > oauthSecretMaxEncodedSize {
			return invalidNativeSecretWorkerRequest()
		}
	default:
		return invalidNativeSecretWorkerRequest()
	}
	return nil
}

func validNativeSecretWorkerKey(service, account string) bool {
	if validOAuthSecretBackendService(service) {
		return oauthSecretAccountPattern.MatchString(account)
	}
	if account != secretUser() {
		return false
	}
	if service == deviceCredentialProbeService ||
		service == deviceCredentialService ||
		service == deviceCredentialPendingService {
		return true
	}
	if len(service) <= len(deviceCredentialService)+1 {
		return false
	}
	if profile := deviceCredentialProfileFromService(service); profile != "" {
		return serverProfileNamePattern.MatchString(profile) &&
			!reservedServerProfileNames[profile]
	}
	return false
}

func deviceCredentialProfileFromService(service string) string {
	pendingPrefix := deviceCredentialPendingService + "."
	if profile := strings.TrimPrefix(service, pendingPrefix); profile != service {
		return profile
	}
	currentPrefix := deviceCredentialService + "."
	if profile := strings.TrimPrefix(service, currentPrefix); profile != service &&
		profile != "pending" && profile != "probe" {
		return profile
	}
	return ""
}

func validateNativeSecretWorkerResponse(
	operation nativeSecretOperation,
	response nativeSecretWorkerResponse,
) error {
	if response.SchemaVersion != nativeSecretWorkerSchema {
		return invalidNativeSecretWorkerResponse()
	}
	if response.ErrorCode != "" {
		if response.Found || len(response.Value) != 0 {
			return invalidNativeSecretWorkerResponse()
		}
		return nil
	}
	if operation == nativeSecretGet {
		if response.Found {
			if len(response.Value) == 0 ||
				len(response.Value) > oauthSecretMaxEncodedSize {
				return invalidNativeSecretWorkerResponse()
			}
		} else if len(response.Value) != 0 {
			return invalidNativeSecretWorkerResponse()
		}
		return nil
	}
	if response.Found || len(response.Value) != 0 {
		return invalidNativeSecretWorkerResponse()
	}
	return nil
}

func nativeSecretWorkerTimeout(
	operation nativeSecretOperation,
	cause error,
) error {
	code := CloudErrTimeout
	if nativeSecretOperationMutates(operation) {
		code = CloudErrSecretOutcomeUnknown
	}
	return newCloudError(code, "access native secure storage", cause)
}

func nativeSecretWorkerFailure(
	operation nativeSecretOperation,
	cause error,
) error {
	if nativeSecretOperationMutates(operation) {
		return newCloudError(
			CloudErrSecretOutcomeUnknown,
			"access native secure storage",
			cause,
		)
	}
	return newCloudError(
		CloudErrSecretStore,
		"access native secure storage",
		cause,
	)
}

func nativeSecretWorkerError(code CloudErrorCode) error {
	switch code {
	case CloudErrSecretStore,
		CloudErrSecretStoreLocked,
		CloudErrSecretUIForbidden,
		CloudErrSecretPromptCanceled,
		CloudErrSecretOutcomeUnknown,
		CloudErrSecretConflict,
		CloudErrUnsupportedPlatform:
		return newCloudError(code, "access native secure storage", nil)
	default:
		return invalidNativeSecretWorkerResponse()
	}
}

func nativeSecretOperationMutates(
	operation nativeSecretOperation,
) bool {
	return operation == nativeSecretSet ||
		operation == nativeSecretDelete ||
		operation == nativeSecretDeleteExact
}

func invalidNativeSecretWorkerRequest() error {
	return newCloudError(
		CloudErrInvalidInput,
		"validate native secure-storage request",
		nil,
	)
}

func invalidNativeSecretWorkerResponse() error {
	return newCloudError(
		CloudErrSecretStore,
		"validate native secure-storage response",
		errors.New("invalid worker response"),
	)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
