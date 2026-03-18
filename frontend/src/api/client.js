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

export function listRecipes(limit = 20, offset = 0) {
  return request(`/recipes?limit=${limit}&offset=${offset}`);
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

export function searchRecipes(params) {
  return request('/recipes/search', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Meal Plans
export function createPlan(name) {
  return request('/plans', { method: 'POST', body: JSON.stringify({ name }) });
}

export function listPlans() {
  return request('/plans');
}

export function getPlan(id) {
  return request(`/plans/${id}`);
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

export function getPlanIngredients(planId) {
  return request(`/plans/${planId}/ingredients`);
}

// Settings
export function listProviders() {
  return request('/settings/providers');
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

export function updateSettings(settings) {
  return request('/settings', {
    method: 'PATCH',
    body: JSON.stringify(settings),
  });
}

export function generateStream(endpoint, body) {
  return fetch(`${API_BASE}/generate/${endpoint}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
