//go:build !darwin && !windows

package main

func platformCloudRemoteSecureStorageBoundaryAvailable() bool {
	return true
}

func platformNativeSecretWorkerParentVerified() bool {
	return false
}

func platformRunNativeSecretWorker(
	_ nativeSecretWorkerRequest,
) nativeSecretWorkerResponse {
	return nativeSecretWorkerResponse{
		SchemaVersion: nativeSecretWorkerSchema,
		ErrorCode:     CloudErrUnsupportedPlatform,
	}
}
