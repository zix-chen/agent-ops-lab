const state = {
  scenarios: [],
  lastRequest: null,
};

const elements = {
  scenarioGrid: document.querySelector("#scenario-grid"),
  emptyRun: document.querySelector("#empty-run"),
  runDetail: document.querySelector("#run-detail"),
  runID: document.querySelector("#run-id"),
  runService: document.querySelector("#run-service"),
  runDiagnosis: document.querySelector("#run-diagnosis"),
  runVerdict: document.querySelector("#run-verdict"),
  runRecommendation: document.querySelector("#run-recommendation"),
  confidenceFill: document.querySelector("#confidence-fill"),
  confidenceValue: document.querySelector("#confidence-value"),
  timeline: document.querySelector("#timeline"),
  replayButton: document.querySelector("#replay-button"),
  evaluateButton: document.querySelector("#evaluate-button"),
  runDefaultButton: document.querySelector("#run-default-button"),
  resetButton: document.querySelector("#reset-button"),
  metricRuns: document.querySelector("#metric-runs"),
  metricSafe: document.querySelector("#metric-safe"),
  metricRetries: document.querySelector("#metric-retries"),
  metricCircuit: document.querySelector("#metric-circuit"),
  scoreRing: document.querySelector("#score-ring"),
  scoreValue: document.querySelector("#score-value"),
  evaluationCopy: document.querySelector("#evaluation-copy"),
  evaluationCases: document.querySelector("#evaluation-cases"),
  toast: document.querySelector("#toast"),
};

async function request(path, options = {}) {
  const response = await fetch(path, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body?.error?.message || `Request failed with ${response.status}`);
  }
  return response.json();
}

function escapeHTML(value) {
  const element = document.createElement("span");
  element.textContent = String(value ?? "");
  return element.innerHTML;
}

function showToast(message) {
  elements.toast.textContent = message;
  elements.toast.classList.add("show");
  window.setTimeout(() => elements.toast.classList.remove("show"), 2400);
}

function renderScenarios(scenarios) {
  elements.scenarioGrid.replaceChildren();
  scenarios.forEach((scenario) => {
    const button = document.createElement("button");
    button.className = "scenario-card";
    button.type = "button";
    button.dataset.scenario = scenario.id;
    button.innerHTML = `
      <div class="scenario-topline">
        <span>${escapeHTML(scenario.severity)}</span>
        <span>${escapeHTML(scenario.service)}</span>
      </div>
      <h3>${escapeHTML(scenario.name)}</h3>
      <p>${escapeHTML(scenario.description)}</p>
      <span class="scenario-signal">${escapeHTML(scenario.signal)}</span>
    `;
    button.addEventListener("click", () => runScenario(scenario));
    elements.scenarioGrid.append(button);
  });
}

async function runScenario(scenario, replay = false) {
  const key = replay && state.lastRequest
    ? state.lastRequest.key
    : `${scenario.id}-${Date.now()}`;
  const incident = {
    id: `inc-${Date.now()}`,
    service: scenario.service,
    severity: scenario.severity,
    symptom: scenario.symptom,
    scenario: scenario.id,
  };
  if (replay && state.lastRequest) {
    incident.id = state.lastRequest.incident.id;
  }

  setBusy(true);
  try {
    const payload = await request("/api/runs", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": key,
      },
      body: JSON.stringify(incident),
    });
    state.lastRequest = { scenario, incident, key };
    renderRun(payload.run);
    elements.replayButton.disabled = false;
    await refreshMetrics();
    showToast(payload.duplicate
      ? "Duplicate alert safely returned the original run."
      : `Run ${payload.run.id} completed.`);
    document.querySelector(".workspace-grid").scrollIntoView({ behavior: "smooth", block: "start" });
  } catch (error) {
    showToast(error.message);
  } finally {
    setBusy(false);
  }
}

function renderRun(run) {
  elements.emptyRun.classList.add("hidden");
  elements.runDetail.classList.remove("hidden");
  elements.runID.textContent = run.id;
  elements.runService.textContent = run.incident.service;
  elements.runDiagnosis.textContent = run.diagnosis;
  elements.runVerdict.textContent = run.status.replaceAll("_", " ");
  elements.runVerdict.className = `verdict ${run.status}`;
  elements.runRecommendation.textContent = run.recommendation;
  const confidence = Math.round(run.confidence * 100);
  elements.confidenceFill.style.width = `${confidence}%`;
  elements.confidenceValue.textContent = `${confidence}%`;

  elements.timeline.replaceChildren();
  run.steps.forEach((step) => {
    const item = document.createElement("li");
    const icon = ["failed", "blocked"].includes(step.status) ? "×" :
      ["warning", "approval_required"].includes(step.status) ? "!" : "✓";
    item.innerHTML = `
      <span class="step-icon ${escapeHTML(step.status)}">${icon}</span>
      <span class="step-name">${escapeHTML(step.name)}</span>
      <span class="step-detail">${escapeHTML(step.detail)}</span>
      <span class="step-attempt">${step.attempts} attempt${step.attempts === 1 ? "" : "s"}</span>
    `;
    elements.timeline.append(item);
  });
}

async function refreshMetrics() {
  const metrics = await request("/api/metrics");
  elements.metricRuns.textContent = metrics.totalRuns;
  elements.metricSafe.textContent = metrics.resolved + metrics.approvalPending;
  elements.metricRetries.textContent = metrics.retried;
  elements.metricCircuit.textContent = metrics.circuitOpen ? "OPEN" : "CLOSED";
  elements.metricCircuit.classList.toggle("open", metrics.circuitOpen);
}

async function runEvaluation() {
  elements.evaluateButton.disabled = true;
  elements.evaluateButton.textContent = "Evaluating…";
  try {
    const report = await request("/api/evaluations/run", { method: "POST" });
    const percentage = Math.round(report.score * 100);
    elements.scoreValue.textContent = `${percentage}%`;
    elements.scoreRing.style.setProperty("--score", `${percentage * 3.6}deg`);
    elements.evaluationCopy.textContent =
      `${report.passed}/${report.total} safety cases passed in ${report.durationMs}ms.`;
    elements.evaluationCases.replaceChildren();
    report.cases.forEach((testCase) => {
      const row = document.createElement("div");
      row.className = "evaluation-case";
      row.innerHTML = `
        <i>${testCase.passed ? "✓" : "×"}</i>
        <div>
          <strong>${escapeHTML(testCase.name)}</strong>
          <span>${escapeHTML(testCase.guardrail)}</span>
        </div>
      `;
      elements.evaluationCases.append(row);
    });
    showToast("Regression suite completed.");
  } catch (error) {
    showToast(error.message);
  } finally {
    elements.evaluateButton.disabled = false;
    elements.evaluateButton.textContent = "Run evaluation suite";
  }
}

async function resetLab() {
  try {
    await request("/api/reset", { method: "POST" });
    state.lastRequest = null;
    elements.emptyRun.classList.remove("hidden");
    elements.runDetail.classList.add("hidden");
    elements.replayButton.disabled = true;
    elements.scoreValue.textContent = "—";
    elements.scoreRing.style.setProperty("--score", "0deg");
    elements.evaluationCopy.textContent =
      "Run the deterministic regression suite before changing prompts, tools or policies.";
    elements.evaluationCases.replaceChildren();
    await refreshMetrics();
    showToast("Lab state reset.");
  } catch (error) {
    showToast(error.message);
  }
}

function setBusy(busy) {
  document.querySelectorAll(".scenario-card").forEach((button) => {
    button.disabled = busy;
  });
  elements.runDefaultButton.disabled = busy;
}

async function boot() {
  try {
    const scenarios = await request("/api/scenarios");
    state.scenarios = scenarios;
    renderScenarios(scenarios);
    await refreshMetrics();
  } catch (error) {
    elements.scenarioGrid.innerHTML = `<div class="loading-card">${escapeHTML(error.message)}</div>`;
  }
}

elements.runDefaultButton.addEventListener("click", () => {
  if (state.scenarios[0]) {
    runScenario(state.scenarios[0]);
  }
});
elements.replayButton.addEventListener("click", () => {
  if (state.lastRequest) {
    runScenario(state.lastRequest.scenario, true);
  }
});
elements.evaluateButton.addEventListener("click", runEvaluation);
elements.resetButton.addEventListener("click", resetLab);

boot();
