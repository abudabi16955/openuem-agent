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

const statusLabels = {
  available: "Dostępny",
  queued: "W kolejce",
  downloading: "Pobieranie",
  installing: "Instalowanie",
  installed: "Aktualna",
  failed: "Błąd"
};

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
  toast.textContent = message;
  toast.style.background = isError ? "#dc2626" : "#1e293b";
  toast.style.display = "block";
  setTimeout(() => {
    toast.style.display = "none";
  }, 3500);
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

function populateCategories() {
  const select = document.getElementById("categorySelect");
  const categories = new Set();
  packages.forEach(p => {
    if (p.category) categories.add(p.category);
  });

  const current = select.value;
  select.innerHTML = '<option value="all">Wszystkie kategorie</option>';
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
    grid.innerHTML = '<div class="empty-state">Brak dostępnych pakietów spełniających kryteria.</div>';
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
      badgeText = statusLabels[taskStatus] || "Przetwarzanie";
      buttonHtml = `<button class="btn" disabled>${badgeText}...</button>`;
    } else if (taskStatus === "failed") {
      badgeClass = "failed";
      badgeText = "Błąd";
      buttonHtml = `<button class="btn" onclick="installPackage('${pkgId}', this)">Ponów próbę</button>`;
    } else if (!installedVer && taskStatus !== "installed") {
      // 1. Brak zainstalowanej wersji: przycisk „Zainstaluj”
      badgeClass = "available";
      badgeText = "Dostępny";
      buttonHtml = `<button class="btn" onclick="installPackage('${pkgId}', this)">Zainstaluj</button>`;
    } else {
      const activeInstalledVer = installedVer || pkgVer;
      const cmp = compareVersions(activeInstalledVer, pkgVer);

      if (cmp < 0) {
        // 2. Zainstalowana wersja jest starsza niż w katalogu: przycisk „Zaktualizuj do vX.Y.Z”
        badgeClass = "upgrade";
        badgeText = `Aktualizacja (zainst. v${activeInstalledVer})`;
        buttonHtml = `<button class="btn btn-upgrade" onclick="installPackage('${pkgId}', this)">Zaktualizuj do v${pkgVer}</button>`;
      } else {
        // 3. Zainstalowana wersja jest równa lub wyższa: status/badge „Aktualna” i zablokowany przycisk
        badgeClass = "installed";
        badgeText = "Aktualna";
        buttonHtml = `<button class="btn" disabled>Aktualna</button>`;
      }
    }

    const card = document.createElement("div");
    card.className = "card";

    const iconHtml = pkg.icon_url
      ? `<img src="${pkg.icon_url}" alt="${pkg.name}" onerror="this.parentElement.textContent='${(pkg.name || '?')[0]}'"/>`
      : (pkg.name ? pkg.name[0].toUpperCase() : "P");

    let metaText = pkg.publisher ? `${pkg.publisher} • ` : "";
    metaText += `v${pkgVer}`;
    if (installedVer) {
      metaText += ` (zainst. v${installedVer})`;
    }

    card.innerHTML = `
      <div class="card-header">
        <div class="card-icon">${iconHtml}</div>
        <div class="card-title">
          <h3>${pkg.name}</h3>
          <div class="meta">${metaText}</div>
        </div>
      </div>
      <div class="card-desc">${pkg.description || "Brak opisu."}</div>
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
    btnElement.textContent = "Wysyłanie...";
  }

  try {
    const res = await fetch("/api/install", {
      method: "POST",
      headers: authHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ package_id: packageId })
    });
    const data = await res.json();
    if (data.success) {
      showToast("Rozpoczęto zadanie instalacji");
      statuses[packageId] = "queued";
      renderPackages();
      fetchStatuses();
    } else {
      pendingInstalls.delete(packageId);
      showToast(data.error || "Błąd instalacji", true);
      renderPackages();
    }
  } catch (err) {
    pendingInstalls.delete(packageId);
    showToast("Błąd połączenia z agentem", true);
    renderPackages();
  }
}

document.addEventListener("DOMContentLoaded", () => {
  const searchInput = document.getElementById("searchInput");
  const categorySelect = document.getElementById("categorySelect");

  searchInput.addEventListener("input", e => {
    searchQuery = e.target.value;
    renderPackages();
  });

  categorySelect.addEventListener("change", e => {
    selectedCategory = e.target.value;
    renderPackages();
  });

  fetchCatalog();
  fetchStatuses();

  // Poll statuses every 3 seconds
  setInterval(fetchStatuses, 3000);
});
