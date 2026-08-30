import { useCallback, useEffect, useState } from 'react';
import { App, Button, Card, Col, Descriptions, Input, Row, Table, Tag, Typography } from 'antd';
import { api } from '../api';
import type { IndexStatus, Me, Task } from '../types';

const taskColor: Record<string, string> = {
  queued: 'default',
  running: 'processing',
  done: 'success',
  failed: 'error',
  cancelled: 'warning',
};

export default function System({ me }: { me: Me }) {
  const { message } = App.useApp();
  const [status, setStatus] = useState<IndexStatus | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [localPath, setLocalPath] = useState('');

  const refresh = useCallback(async () => {
    try {
      setStatus(await api.indexStatus());
      setTasks(await api.tasks());
    } catch (e) {
      message.error((e as Error).message);
    }
  }, [message]);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 5000);
    return () => clearInterval(t);
  }, [refresh]);

  return (
    <Row gutter={16}>
      <Col span={10}>
        <Card title="Bangumi 本地索引（Archive dump）">
          <Descriptions column={1} size="small">
            <Descriptions.Item label="dump 版本">{status?.dump_version || '未导入'}</Descriptions.Item>
            <Descriptions.Item label="导入时间">{status?.imported_at || '—'}</Descriptions.Item>
            <Descriptions.Item label="条目 / 章节 / 关联">
              {status ? `${status.subjects} / ${status.episodes} / ${status.relations}` : '—'}
            </Descriptions.Item>
          </Descriptions>
          <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
            应用内下载约 435MB（每周三更新）；也可指定宿主机已下载的 zip 路径导入。
          </Typography.Paragraph>
          <Input
            placeholder="本地 zip 路径（留空 = 应用内下载）"
            value={localPath}
            onChange={(e) => setLocalPath(e.target.value)}
            style={{ marginBottom: 8 }}
          />
          <Button
            type="primary"
            disabled={!!status?.task_id}
            onClick={async () => {
              const res = await api.refreshIndex(localPath);
              message.success(`导入任务 #${res.task_id} 已入队`);
              refresh();
            }}
          >
            {status?.task_id ? '导入进行中…' : '导入 / 刷新'}
          </Button>
        </Card>
      </Col>
      <Col span={14}>
        <Card title={`任务（版本 ${me.version}）`} extra={<Button size="small" onClick={refresh}>刷新</Button>}>
          <Table
            rowKey="id"
            size="small"
            dataSource={tasks}
            pagination={{ pageSize: 10 }}
            columns={[
              { title: '#', dataIndex: 'id', width: 50 },
              { title: '类型', dataIndex: 'kind', width: 110 },
              {
                title: '状态',
                dataIndex: 'state',
                width: 90,
                render: (v: string) => <Tag color={taskColor[v] ?? 'default'}>{v}</Tag>,
              },
              {
                title: '进度',
                dataIndex: 'progress',
                ellipsis: true,
                render: (p?: Record<string, unknown>) => (p ? JSON.stringify(p) : '—'),
              },
              { title: '错误', dataIndex: 'error', ellipsis: true, render: (v?: string) => v ?? '—' },
            ]}
          />
        </Card>
      </Col>
    </Row>
  );
}
