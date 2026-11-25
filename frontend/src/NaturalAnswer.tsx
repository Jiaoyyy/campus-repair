import type { ReactNode } from 'react';

function cells(line: string) {
  return line.trim().replace(/^\||\|$/g, '').split('|').map(cell => cell.trim());
}

function text(value: string): ReactNode[] {
  return value.split(/(\*\*[^*]+\*\*)/g).map((part, index) =>
    part.startsWith('**') && part.endsWith('**') ? <strong key={index}>{part.slice(2, -2)}</strong> : part
  );
}

export function NaturalAnswer({ body }: { body: string }) {
  const lines = body.split(/\r?\n/);
  const blocks: ReactNode[] = [];
  let i = 0;

  while (i < lines.length) {
    const start = i;
    const line = lines[i++];
    if (!line.trim()) continue;

    const headings = cells(line);
    const separator = i < lines.length ? cells(lines[i]) : [];
    if (headings.length > 1 && separator.length === headings.length && separator.every(cell => /^:?-{3,}:?$/.test(cell))) {
      i++;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes('|') && cells(lines[i]).length === headings.length) rows.push(cells(lines[i++]));
      blocks.push(<div className="answer-table" key={start} tabIndex={0} aria-label="回答表格"><table>
        <thead><tr>{headings.map((cell, column) => <th key={column} scope="col">{text(cell)}</th>)}</tr></thead>
        <tbody>{rows.map((row, index) => <tr key={index}>{row.map((cell, column) => <td key={column}>{text(cell)}</td>)}</tr>)}</tbody>
      </table></div>);
      continue;
    }

    const unordered = /^\s*[-*+]\s+/;
    const ordered = /^\s*\d{1,2}[.)、）]\s*/;
    const marker = unordered.test(line) ? unordered : ordered;
    if (marker.test(line)) {
      const items = [line.replace(marker, '')];
      while (i < lines.length && marker.test(lines[i])) items.push(lines[i++].replace(marker, ''));
      const list = items.map((item, index) => <li key={index}>{text(item)}</li>);
      blocks.push(marker === ordered ? <ol key={start}>{list}</ol> : <ul key={start}>{list}</ul>);
      continue;
    }

    blocks.push(<p key={start}>{text(line.replace(/^#{1,6}\s+/, ''))}</p>);
  }
  return <div className="message-content natural-answer">{blocks}</div>;
}
