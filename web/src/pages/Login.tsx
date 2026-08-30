import { useState } from 'react';
import { Button, Card, Form, Input, Typography } from 'antd';
import { api } from '../api';
import type { Me } from '../types';

export default function Login({ onLogin }: { onLogin: (me: Me) => void }) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  return (
    <div style={{ display: 'flex', justifyContent: 'center', paddingTop: 120 }}>
      <Card title={<Typography.Title level={4} style={{ margin: 0 }}>roxy 登录</Typography.Title>} style={{ width: 360 }}>
        <Form
          layout="vertical"
          onFinish={async (v) => {
            setLoading(true);
            setError('');
            try {
              const me = await api.login(v.username, v.password);
              onLogin(me);
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setLoading(false);
            }
          }}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          {error && (
            <Typography.Text type="danger" style={{ display: 'block', marginBottom: 12 }}>
              {error}
            </Typography.Text>
          )}
          <Button type="primary" htmlType="submit" block loading={loading}>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  );
}
