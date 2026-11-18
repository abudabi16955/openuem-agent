let packages = [];
let statuses = {};
let pendingInstalls = new Set();
let selectedCategory = "all";
let searchQuery = "";

const statusLabels = {
  available: "Dostępny",
  queued: "W kolejce",
  downloading: "Pobieranie",
  installing: "Instalowanie",
  installed: "Zainstalowany",
  failed: "Błąd"
};

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
    const res = await fetch("/api/catalog");
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
    const res = await fetch("/api/status");
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
    const st = statuses[pkg.ID] || "available";
    const isPending = pendingInstalls.has(pkg.ID);
    const label = statusLabels[st] || st;

    const card = document.createElement("div");
    card.className = "card";

    const iconHtml = pkg.icon_url
      ? `<img src="${pkg.icon_url}" alt="${pkg.name}" onerror="this.parentElement.textContent='${(pkg.name || '?')[0]}'"/>`
      : (pkg.name ? pkg.name[0].toUpperCase() : "P");

    let buttonHtml = "";
    if (st === "installed") {
      buttonHtml = `<button class="btn" disabled>Zainstalowany</button>`;
    } else if (st === "installing" || st === "downloading" || st === "queued" || isPending) {
      buttonHtml = `<button class="btn" disabled>${label}...</button>`;
    } else {
      buttonHtml = `<button class="btn" onclick="installPackage('${pkg.ID}', this)">Zainstaluj</button>`;
    }

    card.innerHTML = `
      <div class="card-header">
        <div class="card-icon">${iconHtml}</div>
        <div class="card-title">
          <h3>${pkg.name}</h3>
          <div class="meta">${pkg.publisher ? pkg.publisher + " • " : ""}v${pkg.version || "1.0"}</div>
        </div>
      </div>
      <div class="card-desc">${pkg.description || "Brak opisu."}</div>
      <div class="card-footer">
        <span class="badge ${st}">${label}</span>
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
      headers: { "Content-Type": "application/json" },
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
