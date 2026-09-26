import React, { createContext, useCallback, useContext, useState } from 'react'

const ToastCtx = createContext(null)
export function ToastProvider({ children }) {
  const [toast, setToast] = useState(null)
  const show = useCallback((message, type = 'ok') => {
    setToast({ message, type, key: Date.now() })
    setTimeout(() => setToast(null), 3200)
  }, [])
  return (
    <ToastCtx.Provider value={show}>
      {children}
      {toast && <div key={toast.key} className={'toast ' + toast.type}>{toast.message}</div>}
    </ToastCtx.Provider>
  )
}
export const useToast = () => useContext(ToastCtx)

export function Modal({ title, onClose, children, width }) {
  return (
    <div className="modal-mask" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}>
      <div className="modal" style={width ? { width } : undefined}>
        <h3>{title}</h3>
        {children}
      </div>
    </div>
  )
}

export function Badge({ color = 'gray', children }) {
  return <span className={'badge ' + color}>{children}</span>
}

export function Loading({ text = '加载中…' }) {
  return <div className="empty">{text}</div>
}

export function Empty({ text = '暂无数据' }) {
  return <div className="empty">{text}</div>
}

export function useRefresh(fn, deps = []) {
  const [loading, setLoading] = useState(false)
  const run = useCallback(async (...args) => {
    setLoading(true)
    try {
      return await fn(...args)
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
  return [run, loading]
}

export function Field({ label, children }) {
  return (
    <div className="field">
      {label && <label>{label}</label>}
      {children}
    </div>
  )
}
