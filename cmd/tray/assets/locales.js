// OpenUEM Self-Service Portal Internationalization (i18n)
// Provides complete English (EN) dictionary as standard, with locale detection.

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
  },
  pl: {
    appTitle: "OpenUEM Self-Service Portal",
    catalogSubtitle: "Katalog Oprogramowania",
    searchPlaceholder: "Szukaj aplikacji (nazwa, kategoria, opis)...",
    allCategories: "Wszystkie kategorie",
    loadingCatalog: "Ładowanie katalogu aplikacji...",
    emptyCatalog: "Brak pakietów spełniających kryteria.",
    noDescription: "Brak opisu.",
    syncButton: "Synchronizuj",
    syncTitle: "Synchronizuj katalog z serwerem",
    syncSuccess: "Katalog został pomyślnie zsynchronizowany",
    syncFailed: "Błąd synchronizacji katalogu",
    statusAvailable: "Dostępny",
    statusQueued: "W kolejce",
    statusDownloading: "Pobieranie",
    statusInstalling: "Instalowanie",
    statusInstalled: "Aktualna",
    statusFailed: "Błąd",
    statusProcessing: "Przetwarzanie",
    btnInstall: "Zainstaluj",
    btnRetry: "Ponów próbę",
    btnUpgrade: "Zaktualizuj do v{version}",
    installedMeta: "(zainst. v{version})",
    upgradeBadge: "Aktualizacja (zainst. v{version})",
    installStarted: "Rozpoczęto zadanie instalacji",
    installFailed: "Błąd instalacji",
    connectionError: "Błąd połączenia z agentem",
    sending: "Wysyłanie...",
  }
};

const currentLang = (typeof navigator !== "undefined" && navigator.language && navigator.language.toLowerCase().startsWith("pl")) ? "pl" : "en";

function t(key, params = {}) {
  const dict = locales[currentLang] || locales.en;
  let str = dict[key] || locales.en[key] || key;
  for (const [k, v] of Object.entries(params)) {
    str = str.replace(`{${k}}`, v);
  }
  return str;
}
