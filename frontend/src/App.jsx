import { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import CreateRecipePage from './pages/CreateRecipePage';
import GeneratePage from './pages/GeneratePage';
import ImportPage from './pages/ImportPage';
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
          <Route path="/generate" element={<GeneratePage />} />
          <Route path="/import" element={<ImportPage />} />
          <Route path="/library" element={<LibraryPage />} />
          <Route path="/plans" element={<PlanPage />} />
          <Route path="/plans/:id" element={<PlanPage />} />
          <Route path="/recipe/new" element={<CreateRecipePage />} />
          <Route path="/recipe/:id" element={<RecipePage />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  );
}
