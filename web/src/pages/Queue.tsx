import { useEffect, useState } from 'react';
import { Segmented, Table, Tag, Typography } from 'antd';
import { api } from '../api';
import type { ReviewItem } from '../types';

export default function Queue() {
  const [state, setState] = useState('open');
  const [rows, setRows] = useState<ReviewItem[]>([]);

  useEffect(() => {
    let alive = true;
    const load = () => api.review(state).then((r) => alive && setRows(r)).catch(() => undefined);
    load();
    const t = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [state]);

  return (
    <>
      <Segmented
        style={{ marginBottom: 16 }}
        value={state}
        onChange={(v) => setState(v as string)}
        options={[
          { label: '待审核', value: 'open' },
          { label: '已批准', value: 'approved' },
          { label: '已驳回', value: 'rejected' },
          { label: '全部', value: '' },
        ]}
      />
      <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
        M2 为只读队列；批准/驳回/返工交互在 M3 落地。每 5 秒自动刷新。
      </Typography.Text>
      <Table
        rowKey="case_id"
        dataSource={rows}
        pagination={{ pageSize: 20 }}
        columns={[
          { title: '#', dataIndex: 'case_id', width: 60 },
          { title: '系列', dataIndex: 'series_title', ellipsis: true },
          {
            title: '槽位',
            width: 140,
            render: (_, r) => (
              <span>
                {r.slot_type}
                {r.season != null ? ` S${r.season}` : ''}
                {r.episode != null ? `E${r.episode}` : ''}
                {r.episode_end != null ? `-${r.episode_end}` : ''}
              </span>
            ),
          },
          { title: '文件', dataIndex: 'file_path', ellipsis: true },
          {
            title: '置信度',
            dataIndex: 'confidence',
            width: 90,
            render: (v: number) => <Tag color={v >= 0.9 ? 'green' : v >= 0.7 ? 'orange' : 'red'}>{v.toFixed(2)}</Tag>,
          },
          { title: '原因', dataIndex: 'reason', ellipsis: true },
          { title: '来源', dataIndex: 'decision_source', width: 70 },
          { title: '状态', dataIndex: 'state', width: 90 },
        ]}
      />
    </>
  );
}
