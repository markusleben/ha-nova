const apiVersion = "2026-03-10";
const checkName = "cloud-source-gate";

function fail(message) {
  throw new Error(message);
}

export class ReportedSourceCheckError extends Error {}

function failReported(message) {
  throw new ReportedSourceCheckError(message);
}

function requireSHA(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  return value;
}

export function createCloudSourceCheckReporter({
  appId,
  repository,
  runId,
  token,
}) {
  async function github(endpoint, init = {}) {
    const response = await fetch(`https://api.github.com/${endpoint}`, {
      ...init,
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        "User-Agent": "ha-nova-cloud-source-gate",
        "X-GitHub-Api-Version": apiVersion,
        ...(init.headers ?? {}),
      },
    });
    if (!response.ok) {
      fail(`GitHub API ${endpoint} returned HTTP ${response.status}`);
    }
    return response.json();
  }

  function sourceExternalId(workflowRun, targetSHA) {
    if (
      !Number.isSafeInteger(workflowRun.id) ||
      workflowRun.id <= 0 ||
      !Number.isSafeInteger(workflowRun.run_attempt) ||
      workflowRun.run_attempt <= 0
    ) {
      fail("workflow run id and attempt must be positive integers");
    }
    return `workflow-run:${workflowRun.id}:attempt:${workflowRun.run_attempt}:target:${requireSHA(targetSHA, "source check target SHA")}`;
  }

  async function createCheck(workflowRun, targetSHA) {
    return github(`repos/${repository}/check-runs`, {
      method: "POST",
      body: JSON.stringify({
        name: checkName,
        head_sha: workflowRun.head_sha,
        status: "in_progress",
        external_id: sourceExternalId(workflowRun, targetSHA),
        details_url: `https://github.com/${repository}/actions/runs/${runId}`,
        output: {
          title: "Home Assistant Cloud source verification",
          summary: "Trusted default-branch verification is running.",
        },
      }),
    });
  }

  async function sourceCheckRuns(workflowRun) {
    const checks = [];
    const seenIds = new Set();
    let expectedTotal;
    let seen = 0;
    for (let page = 1; page <= 10; page += 1) {
      const response = await github(
        `repos/${repository}/commits/${workflowRun.head_sha}/check-runs?check_name=${checkName}&filter=all&per_page=100&page=${page}`,
      );
      if (
        !Number.isSafeInteger(response.total_count) ||
        response.total_count < 0 ||
        !Array.isArray(response.check_runs)
      ) {
        fail("source check-run response is invalid");
      }
      if (expectedTotal === undefined) {
        expectedTotal = response.total_count;
      } else if (response.total_count !== expectedTotal) {
        fail("source check-run set changed during pagination");
      }
      for (const candidate of response.check_runs) {
        if (
          !Number.isSafeInteger(candidate.id) ||
          candidate.id <= 0 ||
          seenIds.has(candidate.id)
        ) {
          fail("source check-run pagination returned an invalid duplicate");
        }
        seenIds.add(candidate.id);
        if (candidate.app?.id === appId && candidate.name === checkName) {
          checks.push(candidate);
        }
      }
      seen += response.check_runs.length;
      if (seen === expectedTotal) {
        return checks;
      }
      if (seen > expectedTotal || response.check_runs.length !== 100) {
        fail("source check-run pagination ended before total_count");
      }
    }
    fail("more than 1,000 source check runs exist for the candidate commit");
  }

  async function sourceChecks(workflowRun, targetSHA) {
    const externalId = sourceExternalId(workflowRun, targetSHA);
    return (await sourceCheckRuns(workflowRun)).filter(
      (candidate) => candidate.external_id === externalId,
    );
  }

  function attemptPrefix(workflowRun) {
    const externalId = sourceExternalId(workflowRun, workflowRun.head_sha);
    return externalId.slice(0, externalId.lastIndexOf("target:") + 7);
  }

  async function deletePendingAttemptChecks(workflowRun, keepId) {
    const prefix = attemptPrefix(workflowRun);
    const pending = (await sourceCheckRuns(workflowRun)).filter(
      (candidate) =>
        candidate.status !== "completed" &&
        candidate.id !== keepId &&
        candidate.external_id?.startsWith(prefix),
    );
    for (const candidate of pending) {
      await deleteCheck(candidate.id);
    }
  }

  async function hasTerminalAttemptSuccess(workflowRun) {
    const prefix = attemptPrefix(workflowRun);
    return (await sourceCheckRuns(workflowRun)).some(
      (candidate) =>
        candidate.status === "completed" &&
        candidate.conclusion === "success" &&
        candidate.external_id?.startsWith(prefix),
    );
  }

  async function completeCheck(checkId, conclusion, summary) {
    await github(`repos/${repository}/check-runs/${checkId}`, {
      method: "PATCH",
      body: JSON.stringify({
        status: "completed",
        conclusion,
        output: {
          title:
            conclusion === "success"
              ? "Home Assistant Cloud source verified"
              : "Home Assistant Cloud source rejected",
          summary,
        },
      }),
    });
  }

  async function deleteCheck(checkId) {
    const response = await fetch(
      `https://api.github.com/repos/${repository}/check-runs/${checkId}`,
      {
        method: "DELETE",
        headers: {
          Accept: "application/vnd.github+json",
          Authorization: `Bearer ${token}`,
          "User-Agent": "ha-nova-cloud-source-gate",
          "X-GitHub-Api-Version": apiVersion,
        },
      },
    );
    if (response.status === 404) {
      return;
    }
    if (!response.ok) {
      fail(`GitHub API check-runs/${checkId} returned HTTP ${response.status}`);
    }
  }

  async function createFailSafeCheck(workflowRun, targetSHA, summary) {
    const failed = await createCheck(workflowRun, targetSHA);
    if (
      !Number.isSafeInteger(failed.id) ||
      failed.id <= 0 ||
      failed.app?.id !== appId
    ) {
      fail("dedicated GitHub App returned an invalid fail-safe check run");
    }
    await completeCheck(failed.id, "failure", summary);
  }

  async function ensurePendingCheck(workflowRun, targetSHA) {
    let checks = await sourceChecks(workflowRun, targetSHA);
    const terminals = checks
      .filter((candidate) => candidate.status === "completed")
      .sort((left, right) => left.id - right.id);
    const conclusions = new Set(
      terminals.map((candidate) => candidate.conclusion),
    );
    if (conclusions.has("success") && conclusions.size === 1) {
      for (const pending of checks.filter(
        (candidate) => candidate.status !== "completed",
      )) {
        await deleteCheck(pending.id);
      }
      return {
        check: terminals[terminals.length - 1],
        terminalSuccess: true,
      };
    }
    if (terminals.length > 0 && conclusions.size !== 1) {
      for (const pending of checks.filter(
        (candidate) => candidate.status !== "completed",
      )) {
        await deleteCheck(pending.id);
      }
      await createFailSafeCheck(
        workflowRun,
        targetSHA,
        "Conflicting terminal source checks were detected for this exact target.",
      );
      failReported("source checks have conflicting terminal conclusions");
    }
    for (const failed of terminals) {
      await deleteCheck(failed.id);
    }
    checks = checks.filter((candidate) => candidate.status !== "completed");
    let pending = checks
      .filter((candidate) => candidate.status !== "completed")
      .sort((left, right) => left.id - right.id)[0];
    if (pending === undefined) {
      pending = await createCheck(workflowRun, targetSHA);
      if (
        !Number.isSafeInteger(pending.id) ||
        pending.id <= 0 ||
        pending.app?.id !== appId
      ) {
        fail("dedicated GitHub App returned an invalid check run");
      }
      checks = await sourceChecks(workflowRun, targetSHA);
      const racedTerminals = checks
        .filter((candidate) => candidate.status === "completed")
        .sort((left, right) => left.id - right.id);
      const racedConclusions = new Set(
        racedTerminals.map((candidate) => candidate.conclusion),
      );
      if (racedConclusions.has("success") && racedConclusions.size === 1) {
        for (const racedPending of checks.filter(
          (candidate) => candidate.status !== "completed",
        )) {
          await deleteCheck(racedPending.id);
        }
        return {
          check: racedTerminals[racedTerminals.length - 1],
          terminalSuccess: true,
        };
      }
      if (racedTerminals.length > 0) {
        for (const racedPending of checks.filter(
          (candidate) => candidate.status !== "completed",
        )) {
          await deleteCheck(racedPending.id);
        }
        await createFailSafeCheck(
          workflowRun,
          targetSHA,
          "A terminal source check raced pending-check creation.",
        );
        failReported("terminal source check raced pending-check creation");
      }
      pending = checks
        .filter((candidate) => candidate.status !== "completed")
        .sort((left, right) => left.id - right.id)[0];
      if (pending === undefined) {
        fail("pending source check disappeared during creation");
      }
    }
    const duplicates = checks.filter(
      (candidate) =>
        candidate.status !== "completed" && candidate.id !== pending.id,
    );
    for (const duplicate of duplicates) {
      await deleteCheck(duplicate.id);
    }
    return { check: pending, terminalSuccess: false };
  }

  return {
    completeCheck,
    deletePendingAttemptChecks,
    ensurePendingCheck,
    hasTerminalAttemptSuccess,
  };
}
