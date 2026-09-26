import React, { useEffect, useState, useCallback } from 'react'
import { api, yuan, TYPE_LABEL, DAMAGE_LABEL } from '../api.js'
import { Badge, Modal, Field, Loading, Empty, useToast } from '../components/ui.jsx'

export default function RepairPage({ page }) {
  if (page === 'maintenance') return <Maintenance />
  return <Orders />
}

const RO_STATUS = { queued: ['排队待修', 'orange'], in_progress: ['维修中', 'blue'], waiting_part: ['等配件', 'purple'], done: ['修复可租', 'green'], scrapped: ['已报废', 'red'] }

function Orders() {
  const [orders, setOrders] = useState(null)
  const [detail, setDetail] = useState(null)
  const toast = useToast()
  const load = useCallback(async () => setOrders(await api('GET', '/repair/orders')), [])
  useEffect(() => { load() }, [])
  const [updating, setUpdating] = useState(null)

  const update = async (o, status) => {
    try {
      const d = await api('POST', `/repair/orders/${o.id}/update`, { status, advice: o.advice, repair_cost: Number(o.repair_cost) || 0 })
      toast(d.message); load()
    } catch (e) { toast(e.message, 'error') }
  }

  if (!orders) return <Loading />
  return (
    <div>
      <h2 style={{ fontSize: 18 }}>雪具维修工单</h2>
      <p className="muted small">柜台验损/游客上报自动开单。修复完成雪具重新上架可租；裂纹严重件可直接报废。</p>
      {orders.length === 0 && <Empty text="当前没有待修工单" />}
      <div className="grid grid-2">
        {orders.map(o => (
          <div className="card" key={o.id}>
            <div className="btn-row">
              <h3 style={{ margin: 0 }}>#{o.id} {o.brand} {o.name} {o.size_label}</h3>
              <span className="spacer" style={{ flex: 1 }} />
              <Badge color={(RO_STATUS[o.status] || [, 'gray'])[1]}>{RO_STATUS[o.status]?.[0] || o.status}</Badge>
            </div>
            <div className="small muted">{TYPE_LABEL[o.equipment_type]} · <span className="mono">{o.qr_code}</span> · 累计出租 {o.rental_count} 次</div>
            <div style={{ margin: '8px 0' }}><b>问题：</b>{o.issue}</div>
            <div className="small">系统维修建议：{o.advice}</div>
            {o.damage_type && <div className="small">损坏类型：{DAMAGE_LABEL[o.damage_type]}（{o.severity}）</div>}
            {o.photos?.length > 0 && <div className="photos">{o.photos.map((p, i) => <img key={i} src={p} alt="" />)}</div>}
            <div className="row" style={{ marginTop: 8 }}>
              <Field label="实际维修成本（元）"><input type="number" step="0.01" defaultValue={o.repair_cost}
                onChange={e => { o.repair_cost = e.target.value }} /></Field>
              <Field label="维修说明/保养建议"><input defaultValue={o.advice} onChange={e => { o.advice = e.target.value }} /></Field>
            </div>
            <div className="btn-row" style={{ marginTop: 8 }}>
              {o.status === 'queued' && <button className="btn sm" onClick={() => update(o, 'in_progress')}>开始维修</button>}
              {o.status !== 'done' && o.status !== 'scrapped' && <>
                <button className="btn sm" onClick={() => update(o, 'waiting_part')}>等配件</button>
                <button className="btn sm" onClick={() => update(o, 'done')}>修复上架</button>
              </>}
              {o.status !== 'scrapped' && o.status !== 'done' && <button className="btn sm danger" onClick={() => update(o, 'scrapped')}>报废</button>}
              <span className="spacer" style={{ flex: 1 }} />
              {o.damage_id && <button className="btn sm ghost" onClick={() => setDetail(o)}>关联单据</button>}
            </div>
          </div>
        ))}
      </div>
      {detail && <RelatedRental order={detail} onClose={() => { setDetail(null); load() }} />}
    </div>
  )
}

// 从维修工单反查租赁单：借助 equipment-trace 找 current_rental_item_id 已被置空，这里展示损坏详情即可
function RelatedRental({ order, onClose }) {
  const toast = useToast()
  return (
    <Modal title={`工单 #${order.id} 关联信息`} onClose={onClose}>
      <p>损坏报告 #{order.damage_id}：{DAMAGE_LABEL[order.damage_type]}（{order.severity}）</p>
      <p className="small muted">{order.issue}</p>
      {order.photos?.length > 0 && <div className="photos">{order.photos.map((p, i) => <img key={i} src={p} alt="" />)}</div>}
      <div className="modal-foot"><button className="btn" onClick={onClose}>关闭</button></div>
    </Modal>
  )
}

function Maintenance() {
  const [list, setList] = useState(null)
  const toast = useToast()
  const load = useCallback(async () => setList(await api('GET', '/repair/maintenance')), [])
  useEffect(() => { load() }, [])
  const generate = async () => {
    try { toast((await api('POST', '/repair/maintenance/generate', {})).message); load() }
    catch (e) { toast(e.message, 'error') }
  }
  const resolve = async (id, action) => {
    try { await api('POST', `/repair/maintenance/${id}/resolve`, { action }); toast('提醒已处理'); load() }
    catch (e) { toast(e.message, 'error') }
  }
  if (!list) return <Loading />
  return (
    <div>
      <div className="btn-row">
        <h2 style={{ margin: 0, fontSize: 18 }}>高损耗型号保养提醒</h2>
        <span className="spacer" style={{ flex: 1 }} />
        <button className="btn" onClick={generate}>🔧 按近 30 天损耗数据生成提醒</button>
      </div>
      <p className="muted small">规则：近 30 天同型号损坏 ≥3 起，或损坏率 ≥20%，或发生丢失 → 生成整批保养/盘点提醒。</p>
      {list.length === 0 && <Empty text="暂无保养提醒，点击右上角按钮生成" />}
      <div className="grid grid-2">
        {list.map(m => (
          <div className="card" key={m.id} style={{ opacity: m.status === 'open' ? 1 : 0.6 }}>
            <div className="btn-row">
              <b>{m.brand} {m.name} {m.size_label}</b>
              <span className="spacer" style={{ flex: 1 }} />
              <Badge color={m.status === 'open' ? 'orange' : 'gray'}>{m.status === 'open' ? '待处理' : m.status === 'done' ? '已保养' : '已忽略'}</Badge>
            </div>
            <div className="alert warn" style={{ margin: '8px 0' }}>{m.reason}</div>
            <div className="small">建议行动：{m.action}</div>
            <div className="small muted" style={{ margin: '6px 0' }}>
              库存 {m.metrics.stock} · 损坏 {m.metrics.damages} · 丢失 {m.metrics.lost} ·
              出库 {m.metrics.rentals} · 损坏率 {m.metrics.damage_rate}% · 维修费 {yuan(m.metrics.repair_cost)}
            </div>
            <div className="small muted">生成于 {m.created_at}</div>
            {m.status === 'open' && <div className="btn-row" style={{ marginTop: 8 }}>
              <button className="btn sm" onClick={() => resolve(m.id, 'done')}>已完成保养</button>
              <button className="btn sm ghost" onClick={() => resolve(m.id, 'dismissed')}>忽略</button>
            </div>}
          </div>
        ))}
      </div>
    </div>
  )
}
