let currentIndex = -1;
const cards = [];

function openPanel(cardEl) {
  const container = document.getElementById("detail-panel-container");
  const grid = document.getElementById("results-grid");
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
}

function showUploadTab() {
  document.getElementById("upload-content").classList.remove("hidden");
  document.getElementById("url-content").classList.add("hidden");
  document
    .getElementById("tab-upload")
    .classList.add("text-[#1a73e8]", "border-b-2", "border-[#1a73e8]");
  document.getElementById("tab-upload").classList.remove("text-gray-500");
  document
    .getElementById("tab-url")
    .classList.remove("text-[#1a73e8]", "border-b-2", "border-[#1a73e8]");
  document.getElementById("tab-url").classList.add("text-gray-500");
}

function showUrlTab() {
  document.getElementById("url-content").classList.remove("hidden");
  document.getElementById("upload-content").classList.add("hidden");
  document
    .getElementById("tab-url")
    .classList.add("text-[#1a73e8]", "border-b-2", "border-[#1a73e8]");
  document.getElementById("tab-url").classList.remove("text-gray-500");
  document
    .getElementById("tab-upload")
    .classList.remove("text-[#1a73e8]", "border-b-2", "border-[#1a73e8]");
  document.getElementById("tab-upload").classList.add("text-gray-500");
}

function handleFileSelect(input) {
  if (input.files && input.files[0]) {
    const reader = new FileReader();
    reader.onload = function (e) {
      const dropZoneContent = document.getElementById("drop-zone-content");
      dropZoneContent.innerHTML =
        '<img src="' +
        e.target.result +
        '" class="max-h-48 mx-auto rounded mb-3"/><p class="text-gray-600">Uploading...</p>';
      document
        .getElementById("upload-form")
        .dispatchEvent(new Event("submit", { bubbles: true }));
    };
    reader.readAsDataURL(input.files[0]);
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
  if (evt.detail.target.id === "results-grid") {
    closeUploadModal();
  }
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
