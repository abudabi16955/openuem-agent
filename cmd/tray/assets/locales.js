// OpenUEM Self-Service Portal Internationalization (i18n)
// Standard: 100% English (EN) across all UI elements.

const locales = {
  en: {
    appTitle: "OpenUEM Self-Service Portal",
    catalogSubtitle: "Software Catalog",
    searchPlaceholder: "Search applications (name, category, description)...",
    allCategories: "All Categories",
    loadingCatalog: "Loading software catalog...",
    emptyCatalog: "No software packages found matching your criteria.",
    noDescription: "No description provided.",
    syncButton: "Sync",
    syncTitle: "Synchronize catalog with server",
    syncSuccess: "Catalog synchronized successfully",
    syncFailed: "Failed to sync catalog",
    statusAvailable: "Available",
    statusQueued: "Queued",
    statusDownloading: "Downloading",
    statusInstalling: "Installing",
    statusInstalled: "Up to date",
    statusFailed: "Failed",
    statusProcessing: "Processing",
    btnInstall: "Install",
    btnRetry: "Retry",
    btnUpgrade: "Upgrade to v{version}",
    installedMeta: "(installed v{version})",
    upgradeBadge: "Update available (installed v{version})",
    installStarted: "Installation task queued",
    installFailed: "Installation failed",
    connectionError: "Connection error with local agent",
    sending: "Sending...",
  }
};

const currentLang = "en";

function t(key, params = {}) {
  const dict = locales.en;
  let str = dict[key] || key;
  for (const [k, v] of Object.entries(params)) {
    str = str.replace(`{${k}}`, v);
  }
  return str;
}
