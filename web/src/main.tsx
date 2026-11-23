import React from 'react';
import ReactDOM from 'react-dom/client';
import './styles/app.css';
import App from './App';
import DesktopSetup from './components/DesktopSetup';

const isDesktopSetup =
  window.location.pathname === '/desktop-setup' ||
  new URLSearchParams(window.location.search).get('view') === 'desktop-setup';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    {isDesktopSetup ? <DesktopSetup /> : <App />}
  </React.StrictMode>
);
