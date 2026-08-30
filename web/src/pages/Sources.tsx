import { useCallback, useEffect, useState } from 'react';
import {
  App, Button, Drawer, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography,
} from 'antd';
import { api } from '../api';
import type { Source, SourceFile } from '../types';

const statusColor: Record<string, string> = {
  new: 'default',
  parsing: 'processing',
  parsed: 'warning',
  placed: 'success',
  ignored: 'default',
  error: 'error',
};

export default function Sources() {
  const { message } = App.useApp();
  const [rows, setRows] = useState<Source[]>([]);
  const [loading, setLoading] = useState(false);
  const [adding, setAdding] = useState(false);
  const [filesOf, setFilesOf] = useState<Source | null>(null);
  const [files, setFiles] = useState<SourceFile[]>([]);
  const [form] = Form.useForm();

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setRows(await api.sources());
    } catch (e) {
      message.error((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const openFiles = async (src: Source) => {
    setFilesOf(src);
    setFiles(await api.files(src.id));
  };

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button type="primary" onClick={() => setAdding(true)}>
          添加源
        </Button>
        <Button onClick={refresh}>刷新</Button>
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        dataSource={rows}
        columns={[
          { title: 'ID', dataIndex: 'id', width: 60 },
          { title: '名称', dataIndex: 'name' },
          { title: '路径', dataIndex: 'path', ellipsis: true },
          { title: '类型', dataIndex: 'kind', width: 90 },
          { title: '文件数', dataIndex: 'file_count', width: 80 },
          {
            title: '启用',
            dataIndex: 'enabled',
            width: 80,
            render: (v: boolean, r) => (
              <Switch
                checked={v}
                onChange={async (checked) => {
                  await api.updateSource(r.id, { enabled: checked } as Partial<Source>);
                  refresh();
                }}
              />
            ),
          },
          {
            title: '操作',
            width: 240,
            render: (_, r) => (
              <Space>
                <Button
                  size="small"
                  onClick={async () => {
                    const res = await api.scanSource(r.id);
                    message.success(`扫描任务 #${res.task_id} 已入队`);
                  }}
                >
                  扫描
                </Button>
                <Button size="small" onClick={() => openFiles(r)}>
                  文件
                </Button>
                <Popconfirm
                  title="删除该源？"
                  onConfirm={async () => {
                    try {
                      await api.deleteSource(r.id);
                      refresh();
                    } catch (e) {
                      message.error((e as Error).message);
                    }
                  }}
                >
                  <Button size="small" danger>
                    删除
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title="添加源（下载目录）"
        open={adding}
        onCancel={() => setAdding(false)}
        onOk={() => form.submit()}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={async (v) => {
            try {
              await api.createSource(v.name, v.path, v.kind);
              setAdding(false);
              form.resetFields();
              refresh();
            } catch (e) {
              message.error((e as Error).message);
            }
          }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="例如：BT 下载" />
          </Form.Item>
          <Form.Item name="path" label="目录路径（roxy 容器内可见）" rules={[{ required: true }]}>
            <Input placeholder="/media/downloads" />
          </Form.Item>
          <Form.Item name="kind" label="类型" initialValue="mixed">
            <Select
              options={[
                { value: 'mixed', label: 'mixed（混合）' },
                { value: 'video', label: 'video（仅视频）' },
                { value: 'subtitle', label: 'subtitle（仅字幕）' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={filesOf ? `文件：${filesOf.path}` : ''}
        width={900}
        open={!!filesOf}
        onClose={() => setFilesOf(null)}
      >
        <Table
          rowKey="id"
          size="small"
          dataSource={files}
          pagination={{ pageSize: 20 }}
          columns={[
            {
              title: '文件',
              dataIndex: 'abs_path',
              ellipsis: true,
              render: (v: string) => <Typography.Text copyable={{ text: v }}>{v.split('/').pop()}</Typography.Text>,
            },
            { title: '类别', dataIndex: 'kind', width: 80 },
            {
              title: '状态',
              dataIndex: 'status',
              width: 90,
              render: (v: string) => <Tag color={statusColor[v] ?? 'default'}>{v}</Tag>,
            },
            {
              title: '解析',
              dataIndex: 'parse_result',
              render: (p?: SourceFile['parse_result']) =>
                p ? (
                  <span>
                    {p.title_candidates?.[0] ?? '—'}
                    {p.episode != null ? ` · EP${p.episode}` : ''}
                    {p.version_key ? ` · [${p.version_key}]` : ''}
                    {p.subtitle_lang ? ` · ${p.subtitle_lang}` : ''}
                    {p.batch ? ' · 合集' : ''}
                  </span>
                ) : (
                  '—'
                ),
            },
          ]}
        />
      </Drawer>
    </>
  );
}
