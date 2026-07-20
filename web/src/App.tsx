import { useState } from 'react';
import { SessionsPage } from './pages/SessionsPage';
import { ToastProvider } from './components/Toast';

type Tab = 'sessions' | 'timeline' | 'resources';

function App() {
  const [activeTab, setActiveTab] = useState<Tab>('sessions');

  return (
    <ToastProvider>
      <div className="flex flex-col min-h-screen">
        <header className="bg-bg-secondary px-6 py-4 border-b border-bg-tertiary">
          <h1 className="text-2xl font-semibold text-text-primary">Agent Handler</h1>
        </header>

        <nav className="flex gap-2 bg-bg-secondary px-6 border-b border-bg-tertiary">
          {(['sessions', 'timeline', 'resources'] as Tab[]).map((tab) => (
            <button
              key={tab}
              className={`px-6 py-3 bg-transparent border-none text-[0.95rem] cursor-pointer border-b-2 transition-all duration-200 capitalize
                ${activeTab === tab
                  ? 'text-accent border-b-accent'
                  : 'text-text-secondary border-b-transparent hover:text-text-primary hover:bg-bg-tertiary'
                }`}
              onClick={() => setActiveTab(tab)}
            >
              {tab}
            </button>
          ))}
        </nav>

        <main className="flex-1 p-6 overflow-y-auto max-[400px]:p-4">
          {activeTab === 'sessions' && <SessionsPage />}
          {activeTab === 'timeline' && (
            <div className="flex items-center justify-center min-h-[300px] text-text-secondary text-lg">
              Timeline view coming soon
            </div>
          )}
          {activeTab === 'resources' && (
            <div className="flex items-center justify-center min-h-[300px] text-text-secondary text-lg">
              Resources view coming soon
            </div>
          )}
        </main>
      </div>
    </ToastProvider>
  );
}

export default App;
