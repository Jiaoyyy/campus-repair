import type { Agent, SkillBinding, SkillConfig } from './assistant-api';

type Props = {
  agent: Agent;
  skills: SkillConfig[];
  toolTitles: Record<string, string>;
  onBindingsChange: (bindings: SkillBinding[]) => void;
  onToggle: (skill: SkillConfig) => void;
};

export function SkillSettings({ agent, skills, toolTitles, onBindingsChange, onToggle }: Props) {
  const applicable = skills.filter(skill => skill.roles.includes(agent.role));

  return (
    <section className="settings-section">
      <h2>业务技能</h2>
      <div className="tool-choices">
        {applicable.map(skill => {
          const binding = agent.skill_bindings?.find(item => item.skill_id === skill.id);
          const missing = skill.allowed_tools.filter(name => !(agent.tool_names || []).includes(name));
          return (
            <div key={skill.id}>
              <label className="material-choice">
                <input
                  type="checkbox"
                  checked={!!binding?.enabled}
                  onChange={event => onBindingsChange([
                    ...(agent.skill_bindings || []).filter(item => item.skill_id !== skill.id),
                    { skill_id: skill.id, enabled: event.target.checked },
                  ])}
                />
                <span>
                  <strong>{skill.name}</strong>
                  <small>{skill.purpose}</small>
                  {!skill.enabled && <small>本组织已停用，绑定保存后仍不会执行。</small>}
                  {binding?.enabled && missing.length > 0 && (
                    <small role="status">
                      需要绑定{missing.map(name => toolTitles[name] || name).join('、')}
                    </small>
                  )}
                </span>
              </label>
              <details className="technical-details">
                <summary>{skill.name}执行规则</summary>
                <p>{skill.trigger}</p>
                <p>{skill.output_contract}</p>
                <small>定义版本 {skill.definition_version}</small>
              </details>
            </div>
          );
        })}
      </div>
      <details className="technical-details">
        <summary>管理本组织技能启停</summary>
        <p>操作立即生效，影响本组织所有已绑定该技能的助手。</p>
        {skills.map(skill => (
          <div key={skill.id} className="section-heading">
            <span>{skill.name} · {skill.enabled ? '已启用' : '已停用'}</span>
            <button type="button" onClick={() => onToggle(skill)}>
              {skill.enabled ? '在本组织停用' : '在本组织启用'}{skill.name}
            </button>
          </div>
        ))}
      </details>
    </section>
  );
}
