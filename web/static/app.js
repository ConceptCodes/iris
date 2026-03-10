let currentIndex = -1;
const cards = [];
const supportedUploadTypes = new Set([
  "image/jpeg",
  "image/png",
  "image/webp",
  "image/gif",
  "image/bmp",
  "image/tiff",
]);
const supportedUploadExtensions = [".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".tif", ".tiff"];
const maxUploadBytes = 20 * 1024 * 1024;

function openPanel(cardEl) {
  const container = document.getElementById("detail-panel-container");
  const grid = document.getElementById("results-grid");
  if (!container || !grid) return;
  container.classList.remove("translate-x-full");
  grid.classList.add("panel-open");
  currentIndex = parseInt(cardEl.dataset.index);
  document.querySelectorAll(".image-card").forEach((card, i) => {
    cards[i] = card;
  });
}

function closePanel() {
  const container = document.getElementById("detail-panel-container");
  const grid = document.getElementById("results-grid");
  if (!container || !grid) return;
  container.classList.add("translate-x-full");
  grid.classList.remove("panel-open");
  currentIndex = -1;
}

function navigatePanel(delta) {
  if (currentIndex < 0) return;
  const newIndex = currentIndex + delta;
  if (newIndex >= 0 && newIndex < cards.length) {
    const card = cards[newIndex];
    card.click();
    card.scrollIntoView({ behavior: "smooth", block: "center" });
  }
}

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") closePanel();
  if (e.key === "ArrowRight") navigatePanel(1);
  if (e.key === "ArrowLeft") navigatePanel(-1);
});

function openUploadModal() {
  document.getElementById("upload-modal").classList.remove("hidden");
}

function closeUploadModal() {
  document.getElementById("upload-modal").classList.add("hidden");
  resetUploadState();
}

function showUploadTab() {
  document.getElementById("upload-content").classList.remove("hidden");
  document.getElementById("url-content").classList.add("hidden");
  document.getElementById("tab-upload").classList.add("active");
  document.getElementById("tab-url").classList.remove("active");
}

function showUrlTab() {
  document.getElementById("url-content").classList.remove("hidden");
  document.getElementById("upload-content").classList.add("hidden");
  document.getElementById("tab-url").classList.add("active");
  document.getElementById("tab-upload").classList.remove("active");
}

function setUploadStatus(message, tone) {
  const status = document.getElementById("upload-status");
  if (!status) return;

  status.textContent = message;
  status.classList.remove(
    "hidden",
    "border-red-200",
    "text-red-700",
    "bg-red-50",
    "dark:border-red-900/60",
    "dark:text-red-200",
    "dark:bg-red-950/40",
    "border-blue-200",
    "text-blue-700",
    "bg-blue-50",
    "dark:border-blue-900/60",
    "dark:text-blue-200",
    "dark:bg-blue-950/40"
  );

  if (tone === "error") {
    status.classList.add("border-red-200", "text-red-700", "bg-red-50", "dark:border-red-900/60", "dark:text-red-200", "dark:bg-red-950/40");
  } else {
    status.classList.add("border-blue-200", "text-blue-700", "bg-blue-50", "dark:border-blue-900/60", "dark:text-blue-200", "dark:bg-blue-950/40");
  }
}

function resetDropZoneContent() {
  const dropZoneContent = document.getElementById("drop-zone-content");
  if (!dropZoneContent) return;

  dropZoneContent.innerHTML =
    '<svg class="w-12 h-12 text-gray-400 dark:text-gray-300 mx-auto mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">' +
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"></path>' +
    "</svg>" +
    '<p class="text-gray-600 dark:text-gray-300 mb-2">Drag & drop an image here</p>' +
    '<p class="text-gray-400 dark:text-gray-300 text-sm mb-1">Supported: JPG, PNG, WebP, GIF, BMP, TIFF</p>' +
    '<p class="text-gray-400 dark:text-gray-300 text-sm mb-3">Up to 20 MB</p>' +
    '<button type="button" class="px-4 py-2 bg-[#1a73e8] dark:bg-[#8ab4f8] text-white dark:text-gray-900 text-sm font-medium rounded hover:bg-[#1557b0] dark:hover:bg-[#a1c4fd] transition-colors">Upload from computer</button>';
}

function resetUploadState() {
  const fileInput = document.getElementById("file-input");
  if (fileInput) {
    fileInput.value = "";
  }
  const status = document.getElementById("upload-status");
  if (status) {
    status.textContent = "";
    status.classList.add("hidden");
  }
  resetDropZoneContent();
}

function isSupportedUpload(file) {
  const fileName = file.name.toLowerCase();
  const hasSupportedExtension = supportedUploadExtensions.some((ext) => fileName.endsWith(ext));
  return supportedUploadTypes.has(file.type) || hasSupportedExtension;
}

function handleFileSelect(input) {
  if (input.files && input.files[0]) {
    const file = input.files[0];
    if (!isSupportedUpload(file)) {
      setUploadStatus("Unsupported file type. Use JPG, PNG, WebP, GIF, BMP, or TIFF.", "error");
      input.value = "";
      resetDropZoneContent();
      return;
    }
    if (file.size > maxUploadBytes) {
      setUploadStatus("File is too large. Use an image up to 20 MB.", "error");
      input.value = "";
      resetDropZoneContent();
      return;
    }

    const reader = new FileReader();
    reader.onload = function (e) {
      const dropZoneContent = document.getElementById("drop-zone-content");
      dropZoneContent.innerHTML =
        '<img src="' +
        e.target.result +
        '" alt="" class="max-h-48 mx-auto rounded mb-3"/><p class="text-gray-600 dark:text-gray-300 font-medium">Uploading and searching...</p>';
      setUploadStatus("Uploading image and searching similar results...", "info");
      document.getElementById("upload-form")?.requestSubmit();
    };
    reader.readAsDataURL(file);
  }
}

function handleDrop(event) {
  event.preventDefault();
  const dropZone = document.getElementById("drop-zone");
  dropZone.classList.remove("border-[#1a73e8]");

  if (event.dataTransfer.files && event.dataTransfer.files[0]) {
    const fileInput = document.getElementById("file-input");
    fileInput.files = event.dataTransfer.files;
    handleFileSelect(fileInput);
  }
}

document.getElementById("search-input").addEventListener("input", function () {
  const clearBtn = document.getElementById("clear-btn");
  clearBtn.style.display = this.value ? "inline-flex" : "none";
});

document.body.addEventListener("htmx:beforeSwap", function (evt) {
  if (evt.detail.target.id === "results-region") {
    closeUploadModal();
  }
});

document.body.addEventListener("htmx:responseError", function (evt) {
  const form = evt.detail.elt;
  if (!form || (form.id !== "upload-form" && form.id !== "url-form")) return;

  const xhr = evt.detail.xhr;
  const message = xhr?.responseText?.trim() || "Search failed. Try another supported image.";
  setUploadStatus(message, "error");
});

document.body.addEventListener("htmx:sendError", function (evt) {
  const form = evt.detail.elt;
  if (!form || form.id !== "upload-form") return;
  setUploadStatus("Upload failed before reaching the server. Try again.", "error");
});

document.body.addEventListener("htmx:afterRequest", function (evt) {
  const form = evt.detail.elt;
  if (!form || form.id !== "upload-form") return;
  if (!evt.detail.successful) return;
  resetUploadState();
});
// Theme toggle functionality
function toggleTheme() {
  const html = document.documentElement;
  const isDark = html.classList.contains("dark");
  const theme = isDark ? "light" : "dark";

  if (theme === "dark") {
    html.classList.add("dark");
    localStorage.setItem("theme", "dark");
    document.getElementById("theme-icon-sun")?.classList.remove("hidden");
    document.getElementById("theme-icon-moon")?.classList.add("hidden");
  } else {
    html.classList.remove("dark");
    localStorage.setItem("theme", "light");
    document.getElementById("theme-icon-moon")?.classList.remove("hidden");
    document.getElementById("theme-icon-sun")?.classList.add("hidden");
  }
}

// Initialize theme and event listeners on page load
document.addEventListener("DOMContentLoaded", function () {
  const savedTheme = localStorage.getItem("theme");
  const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  const theme = savedTheme || (prefersDark ? "dark" : "light");

  const html = document.documentElement;
  if (theme === "dark") {
    html.classList.add("dark");
    document.getElementById("theme-icon-sun")?.classList.remove("hidden");
    document.getElementById("theme-icon-moon")?.classList.add("hidden");
  } else {
    html.classList.remove("dark");
    document.getElementById("theme-icon-moon")?.classList.remove("hidden");
    document.getElementById("theme-icon-sun")?.classList.add("hidden");
  }

  // Theme Toggle listener
  const themeToggle = document.getElementById("theme-toggle");
  if (themeToggle) {
    themeToggle.addEventListener("click", toggleTheme);
  }

  // Event Listeners for Upload Modal
  const uploadOverlay = document.getElementById("upload-modal-overlay");
  if (uploadOverlay) {
    uploadOverlay.addEventListener("click", closeUploadModal);
  }

  const closeUploadBtn = document.getElementById("close-upload-modal-btn");
  if (closeUploadBtn) {
    closeUploadBtn.addEventListener("click", closeUploadModal);
  }

  const tabUpload = document.getElementById("tab-upload");
  if (tabUpload) {
    tabUpload.addEventListener("click", showUploadTab);
  }

  const tabUrl = document.getElementById("tab-url");
  if (tabUrl) {
    tabUrl.addEventListener("click", showUrlTab);
  }

  const dropZone = document.getElementById("drop-zone");
  const fileInput = document.getElementById("file-input");
  if (dropZone && fileInput) {
    dropZone.addEventListener("click", () => fileInput.click());
    dropZone.addEventListener("dragover", (e) => {
      e.preventDefault();
      dropZone.classList.add("border-[#1a73e8]");
    });
    dropZone.addEventListener("dragleave", () => {
      dropZone.classList.remove("border-[#1a73e8]");
    });
    dropZone.addEventListener("drop", handleDrop);
  }

  if (fileInput) {
    fileInput.addEventListener("change", (e) => handleFileSelect(e.target));
  }

  const clearBtn = document.getElementById("clear-btn");
  const searchInput = document.getElementById("search-input");
  if (clearBtn && searchInput) {
    clearBtn.addEventListener("click", () => {
      searchInput.value = "";
      clearBtn.style.display = "none";
    });
  }

  const openUploadBtn = document.getElementById("open-upload-modal-btn");
  if (openUploadBtn) {
    openUploadBtn.addEventListener("click", openUploadModal);
  }

  // Prevent empty searches
  const searchForm = document.getElementById("search-form");
  if (searchForm && searchInput) {
    searchForm.addEventListener("submit", (e) => {
      if (!searchInput.value.trim()) {
        e.preventDefault();
        searchInput.focus();
      }
    });
  }

  // Event Delegation for dynamic elements
  document.body.addEventListener("click", (e) => {
    // Detail panel close button
    if (e.target.closest("#close-panel-btn")) {
      closePanel();
    }
    // Image card click
    const card = e.target.closest(".image-card");
    if (card) {
      openPanel(card);
    }
  });

  // Handle Enter key on cards for accessibility
  document.body.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      const card = e.target.closest(".image-card");
      if (card) openPanel(card);
    }
  });
});
