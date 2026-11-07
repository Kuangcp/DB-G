// 从本机的 lucide-react 包里抽出 db-g 用的图标，生成 ui/icons_gen.go。
//
// bun/npm 缓存里就有完整数据，不必联网：
//
//	LUCIDE_DIR=~/.bun/install/cache/lucide-react@1.50.0@@@1/dist/esm/icons \
//	  node tools/lucideicons/main.mjs
//
// 加图标：在下面的 icons 里加一行 Go 变量名 → lucide 名，再跑一次。
import { readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";

const BASE =
  process.env.LUCIDE_DIR ??
  `${homedir()}/.bun/install/cache/lucide-react@1.50.0@@@1/dist/esm/icons/`;
const OUT = new URL("../../ui/icons_gen.go", import.meta.url).pathname;

// goName → lucide 名
const icons = {
  iconHistory: "history",
  iconSettings: "settings",
  iconPlay: "play",
  iconSearch: "search",
  iconFormat: "align-left",
  iconPlug: "plug",
  iconRefresh: "rotate-cw",
  iconTrash: "trash-2",
  iconClose: "x",
  iconCopy: "copy",
  iconEye: "eye",
  iconChevronDown: "chevron-down",
  iconPin: "pin",
  iconSquarePen: "square-pen",
  iconPlus: "plus",
  iconMinus: "minus",
  iconUndo: "undo-2",
  iconFolder: "folder",
  iconFolderOpen: "folder-open",
  iconFolderPlus: "folder-plus",
  iconExpandAll:   "chevrons-down",
  iconCollapseAll: "chevrons-up",
};

// loadIcon 读一个图标的 __iconData；别名文件（只有 re-export）跟着跳过去。
function loadIcon(name, seen = new Set()) {
  if (seen.has(name)) throw new Error("cycle at " + name);
  seen.add(name);
  const text = readFileSync(BASE + name + ".mjs", "utf8");
  const alias = text.match(/export\s*\{\s*default\s*\}\s*from\s*'\.\/([^']+)\.mjs'/);
  if (alias) return loadIcon(alias[1], seen);
  const start = text.indexOf("const __iconData = ");
  if (start < 0) throw new Error("no __iconData in " + name);
  const objStart = text.indexOf("{", start);
  let depth = 0;
  let i = objStart;
  let quote = null;
  for (; i < text.length; i++) {
    const ch = text[i];
    if (quote) {
      if (ch === "\\") {
        i++;
      } else if (ch === quote) {
        quote = null;
      }
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      quote = ch;
    } else if (ch === "{" || ch === "[") {
      depth++;
    } else if (ch === "}" || ch === "]") {
      if (--depth === 0) {
        i++;
        break;
      }
    }
  }
  return new Function("return (" + text.slice(objStart, i) + ")")();
}

const attrOrder = ["d", "cx", "cy", "r", "x", "y", "width", "height", "x1", "y1", "x2", "y2", "points", "rx", "ry"];

function renderNode([tag, attrs]) {
  const parts = attrOrder.filter((k) => attrs[k] !== undefined).map((k) => `${k}="${attrs[k]}"`);
  return `<${tag} ${parts.join(" ")}/>`;
}

const out = [
  "package ui",
  "",
  'import "github.com/egoist/mygo/ui"',
  "",
  "// 图标取自 Lucide v1.50.0 (https://lucide.dev)，ISC 许可：24x24 描线，小尺寸也认得出。",
  "// 由 tools/lucideicons/main.mjs 生成，不要手改。",
  "var (",
];
for (const [goName, lucide] of Object.entries(icons)) {
  const body = loadIcon(lucide).node.map(renderNode).join("");
  const svg =
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
    `stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${body}</svg>`;
  out.push(`\t${goName} = lucideIcon(\`${svg}\`)`);
}
out.push(")", "", "// lucideIcon 解析内嵌在程序里的 SVG，启动时做一次。", "func lucideIcon(svg string) *ui.SVG {", "\treturn ui.MustParseSVG([]byte(svg))", "}", "");
writeFileSync(OUT, out.join("\n"));
console.log("wrote", OUT, "with", Object.keys(icons).length, "icons");
