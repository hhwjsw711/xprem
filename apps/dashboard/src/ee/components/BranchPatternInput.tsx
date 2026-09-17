// Copyright (c) 2026 Axel Marciano (Mercure Technologies). All rights reserved.
// This file is governed by the Mercure Technologies Enterprise Edition License
// (see ee/LICENSE at the repository root); it is NOT covered by the MIT
// license of this repository.

import { useMemo, useRef, useState } from 'react';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';
import { matchBranchPattern } from '@/ee/lib/branchPattern';

// A name field that suggests the names the app already has while still
// accepting anything typed: a rule may name something that does not exist yet,
// and a pattern like "pr-*" never will.
export const NamePatternInput = ({
  value,
  onChange,
  names,
  noun,
  placeholder,
  disabled,
  invalid,
}: {
  value: string;
  onChange: (value: string) => void;
  names: string[];
  noun: { one: string; many: string };
  placeholder: string;
  disabled?: boolean;
  invalid?: boolean;
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const blurTimer = useRef<number | null>(null);

  const suggestions = useMemo(() => {
    const query = value.trim().toLowerCase();
    return names
      .filter(name => name.toLowerCase() !== query)
      .filter(name => !query || name.toLowerCase().includes(query))
      .slice(0, 8);
  }, [names, value]);

  // What the pattern covers right now. Only shown for a wildcard: for a plain
  // name the field already says it, and repeating it would be noise.
  const matched = useMemo(() => {
    if (!value.includes('*')) return null;
    return names.filter(name => matchBranchPattern(value, name));
  }, [names, value]);

  return (
    <div
      className="relative"
      onBlur={event => {
        // Focus moving to a suggestion keeps the list open (keyboard nav).
        if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
        // Let a click on a suggestion land before the list unmounts.
        blurTimer.current = window.setTimeout(() => setIsOpen(false), 120);
      }}>
      <Input
        value={value}
        onChange={event => onChange(event.target.value)}
        onFocus={() => setIsOpen(true)}
        disabled={disabled}
        spellCheck={false}
        placeholder={placeholder}
        aria-invalid={invalid}
        className={cn('font-mono text-xs', invalid && 'border-destructive')}
      />
      {isOpen && suggestions.length > 0 && (
        <ul className="absolute z-50 mt-1 max-h-48 w-full overflow-y-auto rounded-md border bg-popover p-1 shadow-md">
          {suggestions.map(name => (
            <li key={name}>
              <button
                type="button"
                className="w-full rounded-sm px-2 py-1.5 text-left font-mono text-xs hover:bg-accent hover:text-accent-foreground"
                onMouseDown={() => {
                  if (blurTimer.current) window.clearTimeout(blurTimer.current);
                }}
                onClick={() => {
                  onChange(name);
                  setIsOpen(false);
                }}>
                {name}
              </button>
            </li>
          ))}
        </ul>
      )}
      {matched !== null && (
        <p className="mt-1 text-xs text-muted-foreground">
          {matched.length === 0
            ? `Matches no ${noun.one} today. It still applies to ${noun.many} created later.`
            : `Matches ${matched.length} ${matched.length > 1 ? noun.many : noun.one}: ${matched
                .slice(0, 4)
                .join(', ')}${matched.length > 4 ? `, and ${matched.length - 4} more` : ''}`}
        </p>
      )}
    </div>
  );
};

export const BranchPatternInput = ({
  branches,
  ...props
}: {
  value: string;
  onChange: (value: string) => void;
  branches: string[];
  disabled?: boolean;
  invalid?: boolean;
}) => (
  <NamePatternInput
    {...props}
    names={branches}
    noun={{ one: 'branch', many: 'branches' }}
    placeholder="production, or pr-*"
  />
);
