const panels = new Map([
  ["config", { title: "配置" }],
  ["debug", { title: "调试" }],
  ["test", { title: "测试" }],
  ["logs", { title: "日志" }],
]);

let savedConfig = null;
const maxLogLines = 1000;

const els = {
  tabs: document.querySelectorAll(".tab"),
  panels: document.querySelectorAll(".panel"),
  sectionTitle: document.querySelector("#sectionTitle"),
  saveBtn: document.querySelector("#saveBtn"),
  clearLogsBtn: document.querySelector("#clearLogsBtn"),
  startBtn: document.querySelector("#startBtn"),
  stopBtn: document.querySelector("#stopBtn"),
  restartBtn: document.querySelector("#restartBtn"),
  minimizeBtn: document.querySelector("#minimizeBtn"),
  closeBtn: document.querySelector("#closeBtn"),
  runningActions: document.querySelector("#runningActions"),
  statusText: document.querySelector("#statusText"),
  processName: document.querySelector("#processName"),
  backend: document.querySelector("#backend"),
  fps: document.querySelector("#fps"),
  minimapRegion: document.querySelector("#minimapRegion"),
  worldMapPath: document.querySelector("#worldMapPath"),
  chooseWorldMapBtn: document.querySelector("#chooseWorldMapBtn"),
  globalMinimapScale: document.querySelector("#globalMinimapScale"),
  globalMapScale: document.querySelector("#globalMapScale"),
  globalWorkers: document.querySelector("#globalWorkers"),
  globalTimeoutMS: document.querySelector("#globalTimeoutMS"),
  localWorkers: document.querySelector("#localWorkers"),
  localROI: document.querySelector("#localROI"),
  localExpandedROI: document.querySelector("#localExpandedROI"),
  matchThreshold: document.querySelector("#matchThreshold"),
  globalSearchHotkey: document.querySelector("#globalSearchHotkey"),
  autoGlobalSearch: document.querySelector("#autoGlobalSearch"),
  testImagePath: document.querySelector("#testImagePath"),
  testTimeoutMS: document.querySelector("#testTimeoutMS"),
  chooseTestImageBtn: document.querySelector("#chooseTestImageBtn"),
  startMatchTestBtn: document.querySelector("#startMatchTestBtn"),
  stopMatchTestBtn: document.querySelector("#stopMatchTestBtn"),
  matchTestMessage: document.querySelector("#matchTestMessage"),
  matchTestX: document.querySelector("#matchTestX"),
  matchTestY: document.querySelector("#matchTestY"),
  matchTestScore: document.querySelector("#matchTestScore"),
  matchTestElapsed: document.querySelector("#matchTestElapsed"),
  matchPreview: document.querySelector("#matchPreview"),
  matchPreviewImage: document.querySelector("#matchPreviewImage"),
  inspectWindow: document.querySelector("#inspectWindow"),
  logOutput: document.querySelector("#logOutput"),
};

function api() {
  return window.go?.gui?.App;
}

function runtimeApi() {
  return window.runtime;
}

function currentConfig() {
  return {
    process_name: els.processName.value.trim(),
    backend: els.backend.value,
    fps: Number.parseInt(els.fps.value, 10) || 30,
    minimap_region: savedConfig?.minimap_region || { x: 0, y: 0, size: 0 },
    map_matching: {
      world_map_path: els.worldMapPath.value.trim(),
      global_minimap_scale: Number.parseInt(els.globalMinimapScale.value, 10) || 4,
      global_map_scale: Number.parseInt(els.globalMapScale.value, 10) || 8,
      global_workers: Number.parseInt(els.globalWorkers.value, 10) || 4,
      global_timeout_ms: Number.parseInt(els.globalTimeoutMS.value, 10) || 5000,
      local_workers: Number.parseInt(els.localWorkers.value, 10) || 2,
      local_roi: Number.parseInt(els.localROI.value, 10) || 300,
      local_expanded_roi: Number.parseInt(els.localExpandedROI.value, 10) || 400,
      match_threshold: Number.parseFloat(els.matchThreshold.value) || 0.55,
      global_search_hotkey: els.globalSearchHotkey.value.trim() || "Delete",
      auto_global_search: Boolean(els.autoGlobalSearch.checked),
    },
  };
}

function sameConfig(left, right) {
  if (!left || !right) {
    return false;
  }
  return (
    left.process_name === right.process_name &&
    left.backend === right.backend &&
    Number(left.fps) === Number(right.fps) &&
    sameMapMatching(left.map_matching, right.map_matching)
  );
}

function sameMapMatching(left, right) {
  if (!left || !right) {
    return false;
  }
  return (
    left.world_map_path === right.world_map_path &&
    Number(left.global_minimap_scale) === Number(right.global_minimap_scale) &&
    Number(left.global_map_scale) === Number(right.global_map_scale) &&
    Number(left.global_workers) === Number(right.global_workers) &&
    Number(left.global_timeout_ms) === Number(right.global_timeout_ms) &&
    Number(left.local_workers) === Number(right.local_workers) &&
    Number(left.local_roi) === Number(right.local_roi) &&
    Number(left.local_expanded_roi) === Number(right.local_expanded_roi) &&
    Number(left.match_threshold) === Number(right.match_threshold) &&
    left.global_search_hotkey === right.global_search_hotkey &&
    Boolean(left.auto_global_search) === Boolean(right.auto_global_search)
  );
}

function applyConfig(cfg) {
  els.processName.value = cfg.process_name || "WeGame.exe";
  els.backend.value = cfg.backend ?? "";
  els.fps.value = cfg.fps || 30;
  const matching = cfg.map_matching || {};
  els.worldMapPath.value = matching.world_map_path || "";
  els.globalMinimapScale.value = matching.global_minimap_scale || 4;
  els.globalMapScale.value = matching.global_map_scale || 8;
  els.globalWorkers.value = matching.global_workers || 4;
  els.globalTimeoutMS.value = matching.global_timeout_ms || 5000;
  els.localWorkers.value = matching.local_workers || 2;
  els.localROI.value = matching.local_roi || 300;
  els.localExpandedROI.value = matching.local_expanded_roi || 400;
  els.matchThreshold.value = matching.match_threshold || 0.55;
  els.globalSearchHotkey.value = matching.global_search_hotkey || "Delete";
  els.autoGlobalSearch.checked = Boolean(matching.auto_global_search);
  savedConfig = {
    process_name: els.processName.value.trim(),
    backend: els.backend.value,
    fps: Number.parseInt(els.fps.value, 10) || 30,
    minimap_region: cfg.minimap_region || { x: 0, y: 0, size: 0 },
    map_matching: currentConfig().map_matching,
  };
  renderRegion();
  updateDirtyState();
}

function renderRegion() {
  const region = savedConfig?.minimap_region;
  if (region?.size > 0) {
    els.minimapRegion.value = `x=${region.x}, y=${region.y}, size=${region.size}`;
  } else {
    els.minimapRegion.value = "启动后选择";
  }
}

function setRunning(running, message) {
  els.startBtn.hidden = running;
  els.runningActions.hidden = !running;
  els.statusText.textContent = message || (running ? "workflow 正在运行" : "workflow 未运行");
}

function setBusy(isBusy) {
  els.startBtn.disabled = isBusy;
  els.stopBtn.disabled = isBusy;
  els.restartBtn.disabled = isBusy;
  els.saveBtn.disabled = isBusy || !hasDirtyConfig();
}

function hasDirtyConfig() {
  return !sameConfig(currentConfig(), savedConfig);
}

function updateDirtyState() {
  const current = currentConfig();
  const fieldStates = new Map([
    [els.processName, current.process_name !== savedConfig?.process_name],
    [els.backend, current.backend !== savedConfig?.backend],
    [els.fps, Number(current.fps) !== Number(savedConfig?.fps)],
    [els.worldMapPath, current.map_matching.world_map_path !== savedConfig?.map_matching?.world_map_path],
    [els.globalMinimapScale, Number(current.map_matching.global_minimap_scale) !== Number(savedConfig?.map_matching?.global_minimap_scale)],
    [els.globalMapScale, Number(current.map_matching.global_map_scale) !== Number(savedConfig?.map_matching?.global_map_scale)],
    [els.globalWorkers, Number(current.map_matching.global_workers) !== Number(savedConfig?.map_matching?.global_workers)],
    [els.globalTimeoutMS, Number(current.map_matching.global_timeout_ms) !== Number(savedConfig?.map_matching?.global_timeout_ms)],
    [els.localWorkers, Number(current.map_matching.local_workers) !== Number(savedConfig?.map_matching?.local_workers)],
    [els.localROI, Number(current.map_matching.local_roi) !== Number(savedConfig?.map_matching?.local_roi)],
    [els.localExpandedROI, Number(current.map_matching.local_expanded_roi) !== Number(savedConfig?.map_matching?.local_expanded_roi)],
    [els.matchThreshold, Number(current.map_matching.match_threshold) !== Number(savedConfig?.map_matching?.match_threshold)],
    [els.globalSearchHotkey, current.map_matching.global_search_hotkey !== savedConfig?.map_matching?.global_search_hotkey],
    [els.autoGlobalSearch, Boolean(current.map_matching.auto_global_search) !== Boolean(savedConfig?.map_matching?.auto_global_search)],
  ]);

  fieldStates.forEach((dirty, input) => {
    input.closest(".field")?.classList.toggle("dirty", dirty);
  });
  els.saveBtn.disabled = !hasDirtyConfig();
}

function appendLog(line) {
  if (!line) {
    return;
  }
  const atBottom =
    els.logOutput.scrollTop + els.logOutput.clientHeight >= els.logOutput.scrollHeight - 20;
  const lines = els.logOutput.textContent ? els.logOutput.textContent.trimEnd().split("\n") : [];
  lines.push(line);
  if (lines.length > maxLogLines) {
    lines.splice(0, lines.length - maxLogLines);
  }
  els.logOutput.textContent = `${lines.join("\n")}\n`;
  if (atBottom) {
    els.logOutput.scrollTop = els.logOutput.scrollHeight;
  }
}

function showTab(name) {
  const meta = panels.get(name) || panels.get("config");
  els.tabs.forEach((tab) => tab.classList.toggle("active", tab.dataset.tab === name));
  els.panels.forEach((panel) => panel.classList.toggle("active", panel.id === `panel-${name}`));
  els.sectionTitle.textContent = meta.title;
  els.saveBtn.hidden = name !== "config";
  els.clearLogsBtn.hidden = name !== "logs";
}

async function loadInitialState() {
  const backend = api();
  if (!backend) {
    appendLog("Wails bridge 尚未就绪。");
    return;
  }

  const [cfg, status, logs, matchTestStatus] = await Promise.all([
    backend.LoadConfig(),
    backend.Status(),
    backend.Logs(),
    backend.MatchTestStatus(),
  ]);
  const debugStatus = await backend.DebugStatus();
  applyConfig(cfg);
  setRunning(status.running, status.message);
  setDebugStatus(debugStatus);
  setMatchTestStatus(matchTestStatus);
  logs.forEach(appendLog);
}

async function saveConfig() {
  setBusy(true);
  try {
    const cfg = currentConfig();
    await api().SaveConfig(cfg);
    savedConfig = { ...cfg };
    updateDirtyState();
  } finally {
    setBusy(false);
    updateDirtyState();
  }
}

async function startWorkflow() {
  setBusy(true);
  try {
    const status = await api().Start();
    setRunning(status.running, status.message);
  } finally {
    setBusy(false);
    updateDirtyState();
  }
}

async function stopWorkflow() {
  setBusy(true);
  try {
    const status = await api().Stop();
    setRunning(status.running, status.message);
  } finally {
    setBusy(false);
    updateDirtyState();
  }
}

async function restartWorkflow() {
  setBusy(true);
  try {
    const status = await api().Restart();
    setRunning(status.running, status.message);
  } finally {
    setBusy(false);
    updateDirtyState();
  }
}

async function clearLogs() {
  els.logOutput.textContent = "";
  await api().ClearLogs();
}

function setDebugStatus(status) {
  els.inspectWindow.checked = Boolean(status?.inspectWindow);
}

function setMatchTestStatus(status) {
  const running = Boolean(status?.running);
  els.startMatchTestBtn.disabled = running;
  els.stopMatchTestBtn.disabled = !running;
  if (status?.message) {
    els.matchTestMessage.textContent = status.message;
  }
}

function setMatchTestResult(result) {
  if (!result) {
    return;
  }
  els.matchTestMessage.textContent = result.message || "测试完成";
  els.matchTestX.textContent = result.found ? String(result.x) : "-";
  els.matchTestY.textContent = result.found ? String(result.y) : "-";
  els.matchTestScore.textContent = result.found ? Number(result.score).toFixed(3) : "-";
  els.matchTestElapsed.textContent = `${result.elapsedMs || 0} ms`;
  if (result.previewDataUrl) {
    els.matchPreviewImage.src = result.previewDataUrl;
    els.matchPreview.hidden = false;
  } else {
    els.matchPreviewImage.removeAttribute("src");
    els.matchPreview.hidden = true;
  }
  setMatchTestStatus({ running: false, message: result.message || "测试完成" });
}

async function toggleInspectWindow() {
  const status = await api().SetInspectWindow(els.inspectWindow.checked);
  setDebugStatus(status);
}

async function chooseTestImage() {
  const path = await api().SelectMatchTestImage();
  if (path) {
    els.testImagePath.value = path;
  }
}

async function startMatchTest() {
  els.matchTestMessage.textContent = "地图匹配测试正在运行";
  els.matchTestX.textContent = "-";
  els.matchTestY.textContent = "-";
  els.matchTestScore.textContent = "-";
  els.matchTestElapsed.textContent = "-";
  els.matchPreviewImage.removeAttribute("src");
  els.matchPreview.hidden = true;
  const timeout = Number.parseInt(els.testTimeoutMS.value, 10) || 30000;
  const status = await api().StartMapMatchTest(els.testImagePath.value.trim(), timeout);
  setMatchTestStatus(status);
}

async function stopMatchTest() {
  const status = await api().StopMapMatchTest();
  setMatchTestStatus(status);
}

function wireEvents() {
  els.tabs.forEach((tab) => {
    tab.addEventListener("click", () => showTab(tab.dataset.tab));
  });
  els.saveBtn.addEventListener("click", saveConfig);
  els.clearLogsBtn.addEventListener("click", clearLogs);
  els.startBtn.addEventListener("click", startWorkflow);
  els.stopBtn.addEventListener("click", stopWorkflow);
  els.restartBtn.addEventListener("click", restartWorkflow);
  els.inspectWindow.addEventListener("change", toggleInspectWindow);
  els.chooseTestImageBtn.addEventListener("click", chooseTestImage);
  els.startMatchTestBtn.addEventListener("click", startMatchTest);
  els.stopMatchTestBtn.addEventListener("click", stopMatchTest);
  els.minimizeBtn.addEventListener("click", () => api().MinimizeWindow());
  els.closeBtn.addEventListener("click", () => api().CloseWindow());
  [
    els.processName,
    els.backend,
    els.fps,
    els.globalMinimapScale,
    els.globalMapScale,
    els.globalWorkers,
    els.globalTimeoutMS,
    els.localWorkers,
    els.localROI,
    els.localExpandedROI,
    els.matchThreshold,
    els.globalSearchHotkey,
    els.autoGlobalSearch,
  ].forEach((input) => {
    input.addEventListener("input", updateDirtyState);
    input.addEventListener("change", updateDirtyState);
  });

  const rt = runtimeApi();
  if (rt?.EventsOn) {
    rt.EventsOn("log:line", appendLog);
    rt.EventsOn("log:cleared", () => {
      els.logOutput.textContent = "";
    });
    rt.EventsOn("workflow:status", (payload) => {
      setRunning(payload.state === "running", payload.message);
    });
    rt.EventsOn("debug:status", setDebugStatus);
    rt.EventsOn("match-test:status", setMatchTestStatus);
    rt.EventsOn("match-test:result", setMatchTestResult);
  }
}

window.addEventListener("DOMContentLoaded", async () => {
  wireEvents();
  showTab("config");
  await loadInitialState();
});
