(async function () {
  const wrapper = document.querySelector("[data-version-selector]");
  const select = document.querySelector("[data-version-select]");
  if (!wrapper || !select) {
    return;
  }

  try {
    const res = await fetch(new URL("/versions.json", window.location.origin));
    if (!res.ok) {
      throw new Error("failed to load versions");
    }

    const versions = await res.json();
    if (!Array.isArray(versions) || versions.length === 0) {
      wrapper.hidden = true;
      return;
    }

    const pathParts = window.location.pathname.split("/").filter(Boolean);
    const currentVersion = pathParts[0] ? `/${pathParts[0]}/` : "/";
    const remainder = pathParts.slice(1).join("/");

    select.innerHTML = "";
    versions.forEach((version) => {
      const option = document.createElement("option");
      option.value = version.path;
      option.textContent = version.label;
      if (version.path === currentVersion) {
        option.selected = true;
      }
      select.appendChild(option);
    });

    select.addEventListener("change", () => {
      const targetBase = select.value;
      const target = remainder ? `${targetBase}${remainder}/` : targetBase;
      window.location.assign(target);
    });
  } catch (_error) {
    wrapper.hidden = true;
  }
})();
