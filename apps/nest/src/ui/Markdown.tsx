// Markdown for descriptions, comments and chat (plan 6.3). Raw HTML stays
// text (react-markdown's default without rehype-raw), unsafe links are text,
// and every link is classified by links.ts on its resolved origin.
import type { ReactNode } from "react";
import ReactMarkdown, { defaultUrlTransform, type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { classifyLink, imageSrc } from "../links";
import { go, issuePath } from "../router";
import { useWorkspace } from "./context";

// `mention://` survives until links.ts sees it; everything else goes through
// react-markdown's sanitizer, which empties javascript:, data: and the like.
const urlTransform = (url: string) => (url.startsWith("mention://") ? url : defaultUrlTransform(url));

function Link({ href, children }: { href?: string; children?: ReactNode }) {
  const { slug, tree, names } = useWorkspace();
  const link = classifyLink(href, location.origin);
  switch (link.kind) {
    case "issue": {
      const target = tree.byId.get(link.issueId);
      if (!target) return <span>{children}</span>;
      return (
        <a href={issuePath(slug, target.id)} onClick={(e) => { e.preventDefault(); go(issuePath(slug, target.id)); }}>
          {target.identifier}
        </a>
      );
    }
    case "name": {
      const name = link.actor === "member" ? names.member(link.id)
        : link.actor === "agent" ? names.agent(link.id)
        : link.actor === "squad" ? names.squad(link.id)
        : names.project(link.id);
      if (!name) return <span>{children}</span>;
      return <span className="mention">{link.actor === "project" ? name : `@${name}`}</span>;
    }
    case "app":
      return <a href={link.path} onClick={(e) => { e.preventDefault(); go(link.path); }}>{children}</a>;
    case "file":
    case "external":
      return <a href={link.href} target="_blank" rel="noopener noreferrer">{children}</a>;
    case "text":
      return <span>{children}</span>;
  }
}

function Image({ src, alt }: { src?: string; alt?: string }) {
  const resolved = imageSrc(src, location.origin);
  if (!resolved) return <span>{alt ?? ""}</span>;
  return <img src={resolved} alt={alt ?? ""} loading="lazy" className="md-img" />;
}

const components: Components = {
  a: ({ href, children }) => <Link href={href}>{children}</Link>,
  img: ({ src, alt }) => <Image src={typeof src === "string" ? src : undefined} alt={alt} />,
};

export function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown remarkPlugins={[remarkGfm]} urlTransform={urlTransform} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  );
}
