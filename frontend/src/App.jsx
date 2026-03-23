import { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import AddRecipePage from './pages/AddRecipePage';
import InventoryPage from './pages/InventoryPage';
import LibraryPage from './pages/LibraryPage';
import PlanPage from './pages/PlanPage';
import PendingPage from './pages/PendingPage';
import RecipePage from './pages/RecipePage';
import SettingsPage from './pages/SettingsPage';
import { listPendingRecipes } from './api/client';

export default function App() {
  const [pendingCount, setPendingCount] = useState(0);

  const refreshPendingCount = () => {
    listPendingRecipes()
      .then(data => setPendingCount((data || []).length))
      .catch(() => {});
  };

  useEffect(() => { refreshPendingCount(); }, []);

  return (
    <BrowserRouter>
      <Layout pendingCount={pendingCount}>
        <Routes>
          <Route path="/" element={<PendingPage onCountChange={setPendingCount} />} />
          <Route path="/generate" element={<Navigate to="/recipe/new?mode=generate" replace />} />
          <Route path="/import" element={<Navigate to="/recipe/new?mode=import" replace />} />
          <Route path="/library" element={<LibraryPage />} />
          <Route path="/plans" element={<PlanPage />} />
          <Route path="/plans/:id" element={<PlanPage />} />
          <Route path="/recipe/new" element={<AddRecipePage />} />
          <Route path="/recipe/:id" element={<RecipePage />} />
          <Route path="/inventory" element={<InventoryPage />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  );
}
