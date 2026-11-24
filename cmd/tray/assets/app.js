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

    // Support dynamic icons: data URI, HTTP(S) URL, or letter fallback
    const iconSrc = pkg.icon_url || pkg.icon || "";
    let iconHtml = "";
    if (iconSrc && (iconSrc.startsWith("data:image/") || iconSrc.startsWith("http://") || iconSrc.startsWith("https://"))) {
      iconHtml = `<img src="${iconSrc}" alt="${pkg.name || ''}" onerror="this.style.display='none'; this.nextElementSibling.style.display='flex';" /><div style="display:none; width:100%; height:100%; align-items:center; justify-content:center; font-weight:700;">${(pkg.name || '?')[0].toUpperCase()}</div>`;
    } else {
      iconHtml = `<div style="width:100%; height:100%; display:flex; align-items:center; justify-content:center; font-weight:700;">${(pkg.name || '?')[0].toUpperCase()}</div>`;
    }

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
