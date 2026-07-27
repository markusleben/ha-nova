export function createTrustedCIResolver({ fail, github, repository }) {
  return async function requireTrustedCI(workflowRun) {
    if (
      !Number.isSafeInteger(workflowRun.workflow_id) ||
      !Number.isSafeInteger(workflowRun.run_attempt) ||
      workflowRun.run_attempt <= 0
    ) {
      fail("workflow run must identify its source workflow and attempt");
    }
    const trusted = await github(
      `repos/${repository}/actions/workflows/ci.yml`,
    );
    if (
      !Number.isSafeInteger(trusted.id) ||
      trusted.id !== workflowRun.workflow_id ||
      trusted.path !== ".github/workflows/ci.yml"
    ) {
      fail("workflow run did not originate from the trusted CI workflow");
    }
    const current = await github(
      `repos/${repository}/actions/runs/${workflowRun.id}`,
    );
    if (
      current.id !== workflowRun.id ||
      current.workflow_id !== workflowRun.workflow_id ||
      current.event !== workflowRun.event ||
      current.head_sha !== workflowRun.head_sha ||
      current.head_branch !== workflowRun.head_branch
    ) {
      fail("workflow run lifecycle identity changed");
    }
    if (
      !Number.isSafeInteger(current.run_attempt) ||
      current.run_attempt <= 0 ||
      current.run_attempt < workflowRun.run_attempt
    ) {
      fail("workflow run attempt identity regressed");
    }
    return {
      current,
      staleAttempt: current.run_attempt > workflowRun.run_attempt,
    };
  };
}
