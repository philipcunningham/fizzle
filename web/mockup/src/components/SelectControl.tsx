import * as Select from "@radix-ui/react-select";
import { useId } from "react";

interface Props {
  label: string;
  value: string;
  options: string[];
  onChange: (v: string) => void;
}

export function SelectControl({ label, value, options, onChange }: Props) {
  const id = useId();
  return (
    <div className="field">
      <Select.Root value={value} onValueChange={onChange} name={id}>
        <Select.Trigger className="select-trigger" aria-label={label} id={id}>
          <Select.Value />
          <Select.Icon>▾</Select.Icon>
        </Select.Trigger>
        <Select.Portal>
          <Select.Content className="select-content" position="popper">
            <Select.Viewport>
              {options.map((o) => (
                <Select.Item key={o} value={o} className="select-item">
                  <Select.ItemText>{o}</Select.ItemText>
                </Select.Item>
              ))}
            </Select.Viewport>
          </Select.Content>
        </Select.Portal>
      </Select.Root>
      <label htmlFor={id}>{label}</label>
    </div>
  );
}
