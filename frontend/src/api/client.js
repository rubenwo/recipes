const API_BASE = '/api';

async function request(path, options = {}) {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || 'Request failed');
  }
  if (res.status === 204) return null;
  return res.json();
}

export function listRecipes(limit = 20, offset = 0, cuisineType = '') {
  let url = `/recipes?limit=${limit}&offset=${offset}`;
  if (cuisineType) url += `&cuisine_type=${encodeURIComponent(cuisineType)}`;
  return request(url);
}

export function listCuisines() {
  return request('/recipes/cuisines');
}

export function librarySearch(params) {
  return request('/recipes/library-search', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

export function getRecipe(id) {
  return request(`/recipes/${id}`);
}

export function saveRecipe(recipe) {
  return request('/recipes', {
    method: 'POST',
    body: JSON.stringify(recipe),
  });
}

export function deleteRecipe(id) {
  return request(`/recipes/${id}`, { method: 'DELETE' });
}

export function updateRecipeContent(id, ingredients, instructions, cuisineType) {
  return request(`/recipes/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ ingredients, instructions, cuisine_type: cuisineType }),
  });
}

export function updatePendingRecipeContent(id, ingredients, instructions) {
  return request(`/pending/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ ingredients, instructions }),
  });
}

export function getRecipeSuggestions(count = 3) {
  return request(`/recipes/suggestions?count=${count}`);
}

export function searchRecipes(params) {
  return request('/recipes/search', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

export function aiSearchRecipes(query, limit = 50) {
  return request('/recipes/ai-search', {
    method: 'POST',
    body: JSON.stringify({ query, limit }),
  });
}

// Meal Plans
export function createPlan(name) {
  return request('/plans', { method: 'POST', body: JSON.stringify({ name }) });
}

export function listPlans() {
  return request('/plans');
}

export function getPlan(id, options) {
  return request(`/plans/${id}`, options);
}

export function updatePlan(id, data) {
  return request(`/plans/${id}`, { method: 'PATCH', body: JSON.stringify(data) });
}

export function deletePlan(id) {
  return request(`/plans/${id}`, { method: 'DELETE' });
}

export function addRecipeToPlan(planId, recipeId, servings) {
  return request(`/plans/${planId}/recipes`, {
    method: 'POST',
    body: JSON.stringify({ recipe_id: recipeId, servings }),
  });
}

export function removeRecipeFromPlan(planId, recipeId) {
  return request(`/plans/${planId}/recipes/${recipeId}`, { method: 'DELETE' });
}

export function updatePlanRecipe(planId, recipeId, data) {
  return request(`/plans/${planId}/recipes/${recipeId}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export function getPlanSuggestions(planId, ingredients) {
  return request(`/plans/${planId}/suggestions`, {
    method: 'POST',
    body: JSON.stringify({ ingredients }),
  });
}

export function getPlanIngredients(planId, options) {
  return request(`/plans/${planId}/ingredients`, options);
}

export function randomizePlan(planId, servings, excludedIds = []) {
  return request(`/plans/${planId}/randomize`, {
    method: 'POST',
    body: JSON.stringify({ servings, excluded_ids: excludedIds }),
  });
}

export function orderPlanAH(planId) {
  return request(`/plans/${planId}/order/ah`, { method: 'POST' });
}

// Settings
export function listProviders() {
  return request('/settings/providers');
}

export function listModels(host, type = 'ollama') {
  return request(`/settings/models?host=${encodeURIComponent(host)}&type=${encodeURIComponent(type)}`);
}

export function createProvider(provider) {
  return request('/settings/providers', {
    method: 'POST',
    body: JSON.stringify(provider),
  });
}

export function updateProvider(id, provider) {
  return request(`/settings/providers/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(provider),
  });
}

export function deleteProvider(id) {
  return request(`/settings/providers/${id}`, { method: 'DELETE' });
}

export function getSettings() {
  return request('/settings');
}

export function getFeatureStatus() {
  return request('/settings/features');
}

export function updateSettings(settings) {
  return request('/settings', {
    method: 'PATCH',
    body: JSON.stringify(settings),
  });
}

// Pending recipes (background-generated, awaiting user review)
export function listPendingRecipes() {
  return request('/pending');
}

export function approvePendingRecipe(id) {
  return request(`/pending/${id}/approve`, { method: 'POST' });
}

export function rejectPendingRecipe(id) {
  return request(`/pending/${id}`, { method: 'DELETE' });
}

export function fetchRecipeImage(id) {
  return request(`/recipes/${id}/fetch-image`, { method: 'POST' });
}

export function previewImageByTitle(title) {
  return request('/recipes/preview-image', {
    method: 'POST',
    body: JSON.stringify({ title }),
  });
}

export function getChatHistory(recipeId) {
  return request(`/recipes/${recipeId}/chat`);
}

export function sendChatMessage(recipeId, message) {
  return request(`/recipes/${recipeId}/chat`, {
    method: 'POST',
    body: JSON.stringify({ message }),
  });
}

export function findDuplicates() {
  return request('/recipes/duplicates');
}

export function runTranslationNow() {
  return request('/settings/translation/run', { method: 'POST' });
}

export async function generateStream(endpoint, body, signal) {
  const res = await fetch(`${API_BASE}/generate/${endpoint}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || 'Generation failed');
  }
  return res;
}

// ── Inventory ─────────────────────────────────────────
export function listInventory() {
  return request('/inventory');
}

export function createInventoryItem(item) {
  return request('/inventory', { method: 'POST', body: JSON.stringify(item) });
}

export function updateInventoryItem(id, item) {
  return request(`/inventory/${id}`, { method: 'PATCH', body: JSON.stringify(item) });
}

export function deleteInventoryItem(id) {
  return request(`/inventory/${id}`, { method: 'DELETE' });
}

export function scanIngredient(imageFile) {
  const form = new FormData();
  form.append('image', imageFile);
  return fetch(`${API_BASE}/inventory/scan`, { method: 'POST', body: form });
}

export function listPendingScans() {
  return request('/inventory/scans');
}

export function deletePendingScan(id) {
  return request(`/inventory/scans/${id}`, { method: 'DELETE' });
}
