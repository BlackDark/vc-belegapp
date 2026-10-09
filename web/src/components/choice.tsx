import {
  Select,
  SelectContent,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "./ui/select";

export function Choice(props: {
  label: string;
  value: string;
  options: readonly (readonly [string, string])[];
  onChange: (value: string) => void;
}) {
  const text = (value: string) =>
    props.options.find((item) => item[0] === value)?.[1] ?? value;
  return (
    <Select<string>
      value={props.value}
      onChange={(value) => {
        if (value) props.onChange(value);
      }}
      options={props.options.map((item) => item[0])}
      itemComponent={(itemProps) => (
        <SelectItem item={itemProps.item}>
          {text(itemProps.item.rawValue)}
        </SelectItem>
      )}
    >
      <SelectLabel>{props.label}</SelectLabel>
      <SelectTrigger aria-label={props.label}>
        <SelectValue<string>>
          {(state) => text(state.selectedOption() ?? "")}
        </SelectValue>
      </SelectTrigger>
      <SelectContent />
    </Select>
  );
}
