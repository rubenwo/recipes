import { useState, useEffect, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import { getChatHistory, sendChatMessage } from '../api/client';

export default function CookingChat({ recipeId }) {
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [loadingHistory, setLoadingHistory] = useState(true);
  const bottomRef = useRef(null);
  const shouldScrollRef = useRef(false);

  useEffect(() => {
    getChatHistory(recipeId)
      .then(msgs => setMessages(msgs || []))
      .catch(() => {})
      .finally(() => setLoadingHistory(false));
  }, [recipeId]);

  useEffect(() => {
    if (shouldScrollRef.current) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [messages]);

  const handleSend = async (e) => {
    e.preventDefault();
    const message = input.trim();
    if (!message || loading) return;

    shouldScrollRef.current = true;
    setInput('');
    // crypto.randomUUID guarantees no collision between rapid sends — earlier
    // tempId+1 logic clashed if the user re-sent within the same millisecond.
    const userMsgId = crypto.randomUUID();
    const replyMsgId = crypto.randomUUID();
    setMessages(prev => [...prev, { id: userMsgId, role: 'user', content: message }]);
    setLoading(true);

    try {
      const result = await sendChatMessage(recipeId, message);
      setMessages(prev => [...prev, { id: replyMsgId, role: 'assistant', content: result.response }]);
    } catch {
      setMessages(prev => [...prev, { id: replyMsgId, role: 'assistant', content: 'Something went wrong. Please try again.' }]);
    } finally {
      setLoading(false);
    }
  };

  if (loadingHistory) return null;

  return (
    <div className="cooking-chat">
      <h3>Ask the Chef</h3>
      <div className="chat-messages">
        {messages.length === 0 && (
          <p className="chat-empty">Have a question while cooking? Ask here — e.g. "What can I substitute for rice wine?"</p>
        )}
        {messages.map((msg, i) => (
          <div key={msg.id ?? i} className={`chat-message chat-message-${msg.role}`}>
            <span className="chat-role">{msg.role === 'user' ? 'You' : 'Chef'}</span>
            <div className="chat-bubble">
              {msg.role === 'assistant'
                ? <ReactMarkdown>{msg.content}</ReactMarkdown>
                : <p>{msg.content}</p>}
            </div>
          </div>
        ))}
        {loading && (
          <div className="chat-message chat-message-assistant">
            <span className="chat-role">Chef</span>
            <div className="chat-bubble">
              <p className="chat-thinking">Thinking…</p>
            </div>
          </div>
        )}
        <div ref={bottomRef} />
      </div>
      <form className="chat-input-form" onSubmit={handleSend}>
        <input
          type="text"
          value={input}
          onChange={e => setInput(e.target.value)}
          placeholder="Ask a cooking question…"
          disabled={loading}
          className="chat-input"
        />
        <button type="submit" className="btn btn-primary" disabled={loading || !input.trim()}>
          {loading ? '…' : 'Ask'}
        </button>
      </form>
    </div>
  );
}
