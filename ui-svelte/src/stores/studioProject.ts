import { writable } from "svelte/store";

export const studioProjectStorageKey = "llama-studio-active-project";

function initialProject(): string {
	return typeof localStorage === "undefined" ? "" : localStorage.getItem(studioProjectStorageKey) ?? "";
}

export const activeStudioProject = writable(initialProject());

activeStudioProject.subscribe((projectID) => {
	if (typeof localStorage === "undefined") return;
	if (projectID) localStorage.setItem(studioProjectStorageKey, projectID);
	else localStorage.removeItem(studioProjectStorageKey);
});

export function studioProjectHeaders(): Record<string, string> {
	const projectID = typeof localStorage === "undefined" ? "" : localStorage.getItem(studioProjectStorageKey) ?? "";
	return projectID ? { "X-Studio-Project": projectID } : {};
}
