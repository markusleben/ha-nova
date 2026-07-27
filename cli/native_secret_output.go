package main

type cappedSecretOutput struct {
	bytes    []byte
	limit    int
	overflow bool
}

func newCappedSecretOutput(limit int) cappedSecretOutput {
	return cappedSecretOutput{limit: limit}
}

func (output *cappedSecretOutput) Write(value []byte) (int, error) {
	written := len(value)
	remaining := output.limit - len(output.bytes)
	if remaining > 0 {
		if len(value) < remaining {
			remaining = len(value)
		}
		output.bytes = append(output.bytes, value[:remaining]...)
	}
	if len(value) > remaining {
		output.overflow = true
	}
	return written, nil
}

func (output *cappedSecretOutput) Bytes() []byte {
	return output.bytes
}

func (output *cappedSecretOutput) Len() int {
	return len(output.bytes)
}

func (output *cappedSecretOutput) Overflowed() bool {
	return output.overflow
}

func nativeSecretWorkerResponseErrorCode(
	operation nativeSecretOperation,
) CloudErrorCode {
	if nativeSecretOperationMutates(operation) {
		return CloudErrSecretOutcomeUnknown
	}
	return CloudErrSecretStore
}
