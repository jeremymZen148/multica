import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

interface TerminalEmulatorProps {
  /** Called on mount with write/resize callbacks so the parent can pipe IPC data in. */
  onMount: (
    write: (data: string) => void,
    resize: (cols: number, rows: number) => void,
  ) => void;
  /** Subscribe to incoming data (from IPC) to write into xterm. Returns unsubscribe fn. */
  onData: (handler: (data: string) => void) => () => void;
  /** Called when the terminal dimensions change (after fit), so the parent can resize the PTY. */
  onResize?: (cols: number, rows: number) => void;
  /** Called after the terminal opens and fits for the first time. */
  onReady?: () => void;
  className?: string;
}

export function TerminalEmulator({ onMount, onData, onResize, onReady, className }: TerminalEmulatorProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: '"Geist Mono", "Cascadia Code", "Fira Code", monospace',
      fontSize: 13,
      lineHeight: 1.4,
      theme: {
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        cursor: "hsl(var(--foreground))",
        selectionBackground: "hsl(var(--accent))",
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
      allowTransparency: true,
    });

    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(webLinksAddon);
    term.open(containerRef.current);

    termRef.current = term;
    fitRef.current = fitAddon;

    // Slight delay to let the container settle before fitting
    const fitTimer = setTimeout(() => {
      fitAddon.fit();
      onReady?.();
    }, 50);

    const write = (data: string) => term.write(data);
    const resize = (cols: number, rows: number) => {
      term.resize(cols, rows);
    };

    onMount(write, resize);

    const unsubData = onData((data) => term.write(data));

    const observer = new ResizeObserver(() => {
      fitAddon.fit();
      if (onResize) onResize(term.cols, term.rows);
    });
    observer.observe(containerRef.current);

    return () => {
      clearTimeout(fitTimer);
      unsubData();
      observer.disconnect();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  return <div ref={containerRef} className={className} style={{ width: "100%", height: "100%" }} />;
}
