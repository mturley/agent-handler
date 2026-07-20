import { useState } from 'react';
import './App.css';

type Tab = 'sessions' | 'timeline' | 'resources';

function App() {
  const [activeTab, setActiveTab] = useState<Tab>('sessions');

  return (
    <div className="app">
      <header className="header">
        <h1>Agent Handler</h1>
      </header>

      <nav className="tabs">
        <button
          className={`tab ${activeTab === 'sessions' ? 'active' : ''}`}
          onClick={() => setActiveTab('sessions')}
        >
          Sessions
        </button>
        <button
          className={`tab ${activeTab === 'timeline' ? 'active' : ''}`}
          onClick={() => setActiveTab('timeline')}
        >
          Timeline
        </button>
        <button
          className={`tab ${activeTab === 'resources' ? 'active' : ''}`}
          onClick={() => setActiveTab('resources')}
        >
          Resources
        </button>
      </nav>

      <main className="content">
        {activeTab === 'sessions' && (
          <div className="placeholder">Sessions view coming soon</div>
        )}
        {activeTab === 'timeline' && (
          <div className="placeholder">Timeline view coming soon</div>
        )}
        {activeTab === 'resources' && (
          <div className="placeholder">Resources view coming soon</div>
        )}
      </main>
    </div>
  );
}

export default App;
