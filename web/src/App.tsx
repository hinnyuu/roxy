import { useEffect, useState } from 'react';
import { Alert, Button, Layout, Menu, Space, Typography } from 'antd';
import { Link, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { api } from './api';
import type { Me } from './types';
import Login from './pages/Login';
import Sources from './pages/Sources';
import Queue from './pages/Queue';
import System from './pages/System';

export default function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [checked, setChecked] = useState(false);
  const nav = useNavigate();
  const loc = useLocation();

  useEffect(() => {
    api
      .me()
      .then(setMe)
      .catch(() => setMe(null))
      .finally(() => setChecked(true));
  }, []);

  if (!checked) return null;
  if (!me) {
    return (
      <Routes>
        <Route path="/login" element={<Login onLogin={setMe} />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    );
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Header style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
        <Typography.Title level={4} style={{ color: '#fff', margin: 0 }}>
          roxy
        </Typography.Title>
        <Menu
          theme="dark"
          mode="horizontal"
          selectedKeys={[loc.pathname]}
          style={{ flex: 1, minWidth: 200 }}
          items={[
            { key: '/sources', label: <Link to="/sources">源管理</Link> },
            { key: '/queue', label: <Link to="/queue">审核队列</Link> },
            { key: '/system', label: <Link to="/system">系统</Link> },
          ]}
        />
        <Space>
          <Typography.Text type="secondary">{me.username}</Typography.Text>
          <Button
            size="small"
            onClick={async () => {
              await api.logout();
              nav('/login');
            }}
          >
            登出
          </Button>
        </Space>
      </Layout.Header>
      <Layout.Content style={{ padding: 24 }}>
        {me.using_default_credentials && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            message="正在使用默认密码 admin/admin，请尽快修改凭据（D-029）。"
          />
        )}
        <Routes>
          <Route path="/" element={<Navigate to="/sources" replace />} />
          <Route path="/sources" element={<Sources />} />
          <Route path="/queue" element={<Queue />} />
          <Route path="/system" element={<System me={me} />} />
        </Routes>
      </Layout.Content>
    </Layout>
  );
}
