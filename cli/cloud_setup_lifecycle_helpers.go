package main

import "context"

func pendingCloudLifecycleRank(state cloudLifecycleState) (int, bool) {
	switch state {
	case cloudStateAuthorizing:
		return 0, true
	case cloudStateTokenStored:
		return 1, true
	case cloudStateCloudVerified:
		return 2, true
	case cloudStateDeviceBoundOrPaired:
		return 3, true
	default:
		return 0, false
	}
}

func retirePreviousCloudAuthorization(
	ctx context.Context,
	coordinator cloudSetupCoordinator,
	profileID string,
) error {
	retirer, ok := coordinator.(cloudSetupRetirer)
	if !ok {
		return nil
	}
	return retirer.RetirePrevious(ctx, profileID)
}
