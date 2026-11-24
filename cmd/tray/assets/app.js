let packages = [];
let statuses = {};
let pendingInstalls = new Set();
let selectedCategory = "all";
let searchQuery = "";

// S-15: Retrieve session auth token injected by local TrayServer
const apiToken = document.querySelector('meta[name="openuem-token"]')?.getAttribute('content') || '';

function authHeaders(extraHeaders = {}) {
  const headers = { ...extraHeaders };
  if (apiToken) {
    headers['Authorization'] = `Bearer ${apiToken}`;
  }
  return headers;
}

// Built-in standard SVG icons for packages matching console standard icons & popular software
const standardIcons = {
  "package": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="16.5" y1="9.4" x2="7.5" y2="4.21"></line><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path><polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline><line x1="12" y1="22.08" x2="12" y2="12"></line></svg>',
  "globe": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="2" y1="12" x2="22" y2="12"></line><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path></svg>',
  "terminal": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"></polyline><line x1="12" y1="19" x2="20" y2="19"></line></svg>',
  "code": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"></polyline><polyline points="8 6 2 12 8 18"></polyline></svg>',
  "firefox": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><path d="M12 2a10 10 0 0 1 10 10c0 5.52-4.48 10-10 10S2 17.52 2 12a10 10 0 0 1 3-7.14"></path><path d="M12 6a6 6 0 0 1 6 6c0 3.31-2.69 6-6 6a6 6 0 0 1-5.65-4"></path></svg>',
  "chrome": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><circle cx="12" cy="12" r="4"></circle><line x1="21.17" y1="8" x2="12" y2="8"></line><line x1="3.95" y1="6.06" x2="8.54" y2="14"></line><line x1="10.88" y1="21.94" x2="15.46" y2="14"></line></svg>',
  "server": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect><rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect><line x1="6" y1="6" x2="6.01" y2="6"></line><line x1="6" y1="18" x2="6.01" y2="18"></line></svg>',
  "database": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"></ellipse><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"></path><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path></svg>',
  "shield": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path></svg>',
  "shield-check": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path><polyline points="9 12 11 14 15 10"></polyline></svg>',
  "archive": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="21 8 21 21 3 21 3 8"></polyline><rect x="1" y="3" width="22" height="5"></rect><line x1="10" y1="12" x2="14" y2="12"></line></svg>',
  "app-window": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="16" rx="2"></rect><line x1="2" y1="10" x2="22" y2="10"></line></svg>',
  "file-text": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line><polyline points="10 9 9 9 8 9"></polyline></svg>',
  "video": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="23 7 16 12 23 17 23 7"></polygon><rect x="1" y="5" width="15" height="14" rx="2" ry="2"></rect></svg>',
  "play": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>',
  "activity": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline></svg>',
  "settings": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>',
  "music": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l12-2v13"></path><circle cx="6" cy="18" r="3"></circle><circle cx="18" cy="16" r="3"></circle></svg>',
  "download": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path><polyline points="7 10 12 15 17 10"></polyline><line x1="12" y1="15" x2="12" y2="3"></line></svg>',
  "cloud": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"></path></svg>',
  "folder": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>',
  "monitor": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect><line x1="8" y1="21" x2="16" y2="21"></line><line x1="12" y1="17" x2="12" y2="21"></line></svg>',
  "layers": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"></polygon><polyline points="2 17 12 22 22 17"></polyline><polyline points="2 12 12 17 22 12"></polyline></svg>',
  "lock": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>',
  "cpu": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"></rect><rect x="9" y="9" width="6" height="6"></rect><line x1="9" y1="1" x2="9" y2="4"></line><line x1="15" y1="1" x2="15" y2="4"></line><line x1="9" y1="20" x2="9" y2="23"></line><line x1="15" y1="20" x2="15" y2="23"></line><line x1="20" y1="9" x2="23" y2="9"></line><line x1="20" y1="14" x2="23" y2="14"></line><line x1="1" y1="9" x2="4" y2="9"></line><line x1="1" y1="14" x2="4" y2="14"></line></svg>',
  "zap": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon></svg>',
  "check-circle": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>',
  "wifi": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.55a11 11 0 0 1 14.08 0"></path><path d="M1.42 9a16 16 0 0 1 21.16 0"></path><path d="M8.53 16.11a6 6 0 0 1 6.95 0"></path><line x1="12" y1="20" x2="12.01" y2="20"></line></svg>',
  "printer": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 6 2 18 2 18 9"></polyline><path d="M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2"></path><rect x="6" y="14" width="12" height="8"></rect></svg>',
  "slack": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="13" y="2" width="3" height="8" rx="1.5"></rect><path d="M19 8.5a1.5 1.5 0 0 1-1.5 1.5H16"></path><rect x="8" y="14" width="3" height="8" rx="1.5"></rect><path d="M5 15.5A1.5 1.5 0 0 1 6.5 14H8"></path><rect x="14" y="13" width="8" height="3" rx="1.5"></rect><path d="M15.5 19a1.5 1.5 0 0 1-1.5-1.5V16"></path><rect x="2" y="8" width="8" height="3" rx="1.5"></rect><path d="M8.5 5A1.5 1.5 0 0 1 10 6.5V8"></path></svg>',
  "git": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="18" cy="18" r="3"></circle><circle cx="6" cy="6" r="3"></circle><path d="M6 9v12"></path><path d="M18 9a9 9 0 0 0-9 9"></path></svg>',
  "teams": '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path><circle cx="9" cy="7" r="4"></circle><path d="M23 21v-2a4 4 0 0 0-3-3.87"></path><path d="M16 3.13a4 4 0 0 1 0 7.75"></path></svg>',
};

function renderIcon(pkg) {
  const iconRaw = (pkg.icon_url || pkg.icon || "").trim();
  const name = pkg.name || "";

  // 1. Data URI or HTTP(S) URL
  if (iconRaw.startsWith("data:image/") || iconRaw.startsWith("http://") || iconRaw.startsWith("https://")) {
    const fallbackSvg = standardIcons["package"];
    return `<img src="${iconRaw}" alt="${name}" onerror="this.style.display='none'; this.nextElementSibling.style.display='flex';" /><div style="display:none; width:100%; height:100%; align-items:center; justify-content:center;">${fallbackSvg}</div>`;
  }

  // 2. Exact match with standard icon name
  const lowerIcon = iconRaw.toLowerCase();
  if (standardIcons[lowerIcon]) {
    return standardIcons[lowerIcon];
  }

  // 3. Fallback based on application name keywords
  const lowerName = name.toLowerCase();
  for (const [key, svg] of Object.entries(standardIcons)) {
    if (lowerName.includes(key)) {
      return svg;
    }
  }

  // 4. Default fallback SVG (box package) to prevent empty circle with single letter
  return standardIcons["package"];
}

function getStatusLabel(status) {
  switch (status) {
    case "available":
      return t("statusAvailable");
    case "queued":
      return t("statusQueued");
    case "downloading":
      return t("statusDownloading");
    case "installing":
      return t("statusInstalling");
    case "installed":
      return t("statusInstalled");
    case "failed":
      return t("statusFailed");
    default:
      return t("statusProcessing");
  }
}

// Compare two version strings (semver / numeric / alphanumeric)
function compareVersions(v1, v2) {
  if (!v1 && !v2) return 0;
  if (!v1) return -1;
  if (!v2) return 1;

  const clean = v => String(v).trim().replace(/^v/i, '');
  const p1 = clean(v1).split(/[\.\-_+]/);
  const p2 = clean(v2).split(/[\.\-_+]/);
  const maxLen = Math.max(p1.length, p2.length);

  for (let i = 0; i < maxLen; i++) {
    const s1 = p1[i] || '';
    const s2 = p2[i] || '';
    const n1 = parseInt(s1, 10);
    const n2 = parseInt(s2, 10);

    const isNum1 = !isNaN(n1) && /^\d+$/.test(s1);
    const isNum2 = !isNaN(n2) && /^\d+$/.test(s2);

    if (isNum1 && isNum2) {
      if (n1 < n2) return -1;
      if (n1 > n2) return 1;
      continue;
    }
    if (isNum1 && !isNum2) return 1;
    if (!isNum1 && isNum2) return -1;

    const cmp = s1.toLowerCase().localeCompare(s2.toLowerCase());
    if (cmp !== 0) return cmp;
  }
  return 0;
}

function showToast(message, isError = false) {
  const toast = document.getElementById("toast");
  if (!toast) return;
  toast.textContent = message;
  toast.style.background = isError ? "#dc2626" : "#1e293b";
  toast.style.display = "block";
  setTimeout(() => {
    toast.style.display = "none";
  }, 3500);
}

function applyLocales() {
  const appTitle = document.getElementById("appTitle");
  if (appTitle) appTitle.textContent = t("appTitle");
  const catalogSubtitle = document.getElementById("catalogSubtitle");
  if (catalogSubtitle) catalogSubtitle.textContent = t("catalogSubtitle");
  const searchInput = document.getElementById("searchInput");
  if (searchInput) searchInput.placeholder = t("searchPlaceholder");
  const syncBtn = document.getElementById("syncBtn");
  if (syncBtn) syncBtn.title = t("syncTitle");
  const syncBtnText = document.getElementById("syncBtnText");
  if (syncBtnText) syncBtnText.textContent = t("syncButton");
  const loadingState = document.getElementById("loadingState");
  if (loadingState) loadingState.textContent = t("loadingCatalog");
}

async function fetchCatalog() {
  try {
    const res = await fetch("/api/catalog", {
      headers: authHeaders()
    });
    const data = await res.json();
    if (data.success && data.catalog && data.catalog.packages) {
      packages = data.catalog.packages;
      populateCategories();
      renderPackages();
    }
  } catch (err) {
    console.error("Failed to fetch catalog:", err);
  }
}

async function fetchStatuses() {
  try {
    const res = await fetch("/api/status", {
      headers: authHeaders()
    });
    const data = await res.json();
    if (data.success && data.tasks) {
      data.tasks.forEach(task => {
        statuses[task.package_id] = task.status;
        if (task.status === "installed" || task.status === "failed") {
          pendingInstalls.delete(task.package_id);
        }
      });
      renderPackages();
    }
  } catch (err) {
    console.error("Failed to fetch statuses:", err);
  }
}

async function syncCatalog() {
  const syncBtn = document.getElementById("syncBtn");
  if (syncBtn) {
    syncBtn.classList.add("spinning");
    syncBtn.disabled = true;
  }
  try {
    const res = await fetch("/api/sync", {
      method: "POST",
      headers: authHeaders({ "Content-Type": "application/json" })
    });
    const data = await res.json();
    if (data.success) {
      showToast(t("syncSuccess"));
      if (data.catalog && data.catalog.packages) {
        packages = data.catalog.packages;
        populateCategories();
        renderPackages();
      } else {
        await fetchCatalog();
      }
      await fetchStatuses();
    } else {
      showToast(data.error || t("syncFailed"), true);
    }
  } catch (err) {
    console.error("Sync error:", err);
    showToast(t("syncFailed"), true);
  } finally {
    if (syncBtn) {
      syncBtn.classList.remove("spinning");
      syncBtn.disabled = false;
    }
  }
}

function populateCategories() {
  const select = document.getElementById("categorySelect");
  if (!select) return;
  const categories = new Set();
  packages.forEach(p => {
    if (p.category) categories.add(p.category);
  });

  const current = select.value;
  select.innerHTML = `<option value="all">${t("allCategories")}</option>`;
  categories.forEach(cat => {
    const opt = document.createElement("option");
    opt.value = cat;
    opt.textContent = cat;
    select.appendChild(opt);
  });
  select.value = current || "all";
}

function renderPackages() {
  const grid = document.getElementById("packageGrid");
  if (!grid) return;

  const filtered = packages.filter(pkg => {
    const matchesCategory = selectedCategory === "all" || pkg.category === selectedCategory;
    const query = searchQuery.toLowerCase();
    const matchesSearch = !query ||
      (pkg.name && pkg.name.toLowerCase().includes(query)) ||
      (pkg.description && pkg.description.toLowerCase().includes(query)) ||
      (pkg.publisher && pkg.publisher.toLowerCase().includes(query));
    return matchesCategory && matchesSearch;
  });

  if (filtered.length === 0) {
    grid.innerHTML = `<div class="empty-state">${t("emptyCatalog")}</div>`;
    return;
  }

  grid.innerHTML = "";
  filtered.forEach(pkg => {
    const pkgId = pkg.id || pkg.ID;
    const pkgVer = pkg.version || "1.0";
    const installedVer = pkg.installed_version || "";
    const taskStatus = statuses[pkgId] || "";
    const isPending = pendingInstalls.has(pkgId);

    let badgeText = "";
    let badgeClass = "";
    let buttonHtml = "";

    if (isPending || taskStatus === "queued" || taskStatus === "downloading" || taskStatus === "installing") {
      badgeClass = taskStatus === "queued" ? "queued" : (taskStatus === "downloading" ? "downloading" : "installing");
      badgeText = getStatusLabel(taskStatus);
      buttonHtml = `<button class="btn" disabled>${badgeText}...</button>`;
    } else if (taskStatus === "failed") {
      badgeClass = "failed";
      badgeText = t("statusFailed");
      buttonHtml = `<button class="btn" onclick="installPackage('${pkgId}', this)">${t("btnRetry")}</button>`;
    } else if (!installedVer && taskStatus !== "installed") {
      badgeClass = "available";
      badgeText = t("statusAvailable");
      buttonHtml = `<button class="btn" onclick="installPackage('${pkgId}', this)">${t("btnInstall")}</button>`;
    } else {
      const activeInstalledVer = installedVer || pkgVer;
      const cmp = compareVersions(activeInstalledVer, pkgVer);

      if (cmp < 0) {
        badgeClass = "upgrade";
        badgeText = t("upgradeBadge", { version: activeInstalledVer });
        buttonHtml = `<button class="btn btn-upgrade" onclick="installPackage('${pkgId}', this)">${t("btnUpgrade", { version: pkgVer })}</button>`;
      } else {
        badgeClass = "installed";
        badgeText = t("statusInstalled");
        buttonHtml = `<button class="btn" disabled>${t("statusInstalled")}</button>`;
      }
    }

    const card = document.createElement("div");
    card.className = "card";

    const iconHtml = renderIcon(pkg);

    let metaText = pkg.publisher ? `${pkg.publisher} • ` : "";
    metaText += `v${pkgVer}`;
    if (installedVer) {
      metaText += ` ${t("installedMeta", { version: installedVer })}`;
    }

    card.innerHTML = `
      <div class="card-header">
        <div class="card-icon">${iconHtml}</div>
        <div class="card-title">
          <h3>${pkg.name}</h3>
          <div class="meta">${metaText}</div>
        </div>
      </div>
      <div class="card-desc">${pkg.description || t("noDescription")}</div>
      <div class="card-footer">
        <span class="badge ${badgeClass}">${badgeText}</span>
        ${buttonHtml}
      </div>
    `;

    grid.appendChild(card);
  });
}

// 1-click install with debouncing
async function installPackage(packageId, btnElement) {
  if (pendingInstalls.has(packageId)) {
    return;
  }

  pendingInstalls.add(packageId);
  if (btnElement) {
    btnElement.disabled = true;
    btnElement.textContent = t("sending");
  }

  try {
    const res = await fetch("/api/install", {
      method: "POST",
      headers: authHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ package_id: packageId })
    });
    const data = await res.json();
    if (data.success) {
      showToast(t("installStarted"));
      statuses[packageId] = "queued";
      renderPackages();
      fetchStatuses();
    } else {
      pendingInstalls.delete(packageId);
      showToast(data.error || t("installFailed"), true);
      renderPackages();
    }
  } catch (err) {
    pendingInstalls.delete(packageId);
    showToast(t("connectionError"), true);
    renderPackages();
  }
}

document.addEventListener("DOMContentLoaded", () => {
  applyLocales();

  const searchInput = document.getElementById("searchInput");
  const categorySelect = document.getElementById("categorySelect");

  if (searchInput) {
    searchInput.addEventListener("input", e => {
      searchQuery = e.target.value;
      renderPackages();
    });
  }

  if (categorySelect) {
    categorySelect.addEventListener("change", e => {
      selectedCategory = e.target.value;
      renderPackages();
    });
  }

  fetchCatalog();
  fetchStatuses();

  // Poll statuses every 3 seconds
  setInterval(fetchStatuses, 3000);
});
