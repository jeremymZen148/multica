import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

interface TerminalEmulatorProps {
  onMount: (write: (data: string) => void, resize: (cols: number, rows: number) => void) => void;
  onData: (handler: (data: string) => void) => () => void;
  onInput?: (data: string) => void;
  onResize?: (cols: number, rows: number) => void;
  onReady?: (cols: number, rows: number) => void;
  className?: string;
}

export function TerminalEmulator({ onMount, onData, onInput, onResize, onReady, className }: TerminalEmulatorProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const onInputRef = useRef(onInput);
  onInputRef.current = onInput;
  const onReadyRef = useRef(onReady);
  onReadyRef.current = onReady;
  const onResizeRef = useRef(onResize);
  onResizeRef.current = onResize;

  useEffect(() => {
    if (!containerRef.current) return;

    let term: Terminal | null = null;
    let fitAddon: FitAddon | null = null;
    let unsubData: (() => void) | null = null;
    let readyFired = false;

    const initXterm = (container: HTMLDivElement) => {
      term = new Terminal({
        cursorBlink: true,
        fontFamily: '"Geist Mono", "Cascadia Code", "Fira Code", monospace',
        fontSize: 13,
        lineHeight: 1.4,
        theme: {
          background: "#1e1e2e",
          foreground: "#cdd6f4",
          cursor: "#f5e0dc",
          cursorAccent: "#1e1e2e",
          selectionBackground: "#585b70",
          black: "#1e1e2e",
          red: "#f38ba8",
          green: "#a6e3a1",
          yellow: "#f9e2af",
          blue: "#89b4fa",
          magenta: "#cba6f7",
          cyan: "#89dceb",
          white: "#cdd6f4",
          brightBlack: "#585b70",
          brightRed: "#f38ba8",
          brightGreen: "#a6e3a1",
          brightYellow: "#f9e2af",
          brightBlue: "#89b4fa",
          brightMagenta: "#cba6f7",
          brightCyan: "#89dceb",
          brightWhite: "#cdd6f4",
        },
      });
      fitAddon = new FitAddon();
      term.loadAddon(fitAddon);
      term.loadAddon(new WebLinksAddon());
      term.open(container);
      term.onData((data) => onInputRef.current?.(data));

      onMount(
        (data) => term!.write(data),
        (cols, rows) => term!.resize(cols, rows),
      );
      unsubData = onData((data) => term!.write(data));
    };

    const observer = new ResizeObserver(() => {
      if (!containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      // Require a meaningful height so we don't initialize xterm at 1-2px
      // during the panel open animation (which gives rows=1 and a broken PTY).
      if (rect.width === 0 || rect.height < 50) return;

      if (!term) initXterm(containerRef.current);

      fitAddon!.fit();
      onResizeRef.current?.(term!.cols, term!.rows);

      if (!readyFired) {
        readyFired = true;
        onReadyRef.current?.(term!.cols, term!.rows);
        term!.focus();
      }
    });

    observer.observe(containerRef.current);

    return () => {
      observer.disconnect();
      unsubData?.();
      term?.dispose();
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  return <div ref={containerRef} className={className} style={{ width: "100%", height: "100%" }} />;
}
