import { usePublicLinkURL } from "@/components/utils/use-public-link-url";
import { Copy01Icon, Tick02Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { IconButton, Tooltip } from "@mui/material";
import { useClipboardCopy } from "ente-base/components/utils/hooks";
import type { Collection } from "ente-media/collection";
import { useSettingsSnapshot } from "ente-new/photos/components/utils/use-snapshot";
import { t } from "i18next";

export const LinkCopyChip = ({ collection }: { collection: Collection }) => {
    const { customDomain } = useSettingsSnapshot();
    const { resolvedURL } = usePublicLinkURL(
        collection,
        collection.publicURLs[0],
        customDomain,
    );
    const [copied, copy] = useClipboardCopy(resolvedURL ?? "");
    const label = t(copied ? "copied" : "copy_link");
    return (
        <Tooltip title={label}>
            <span style={{ position: "absolute", top: 8, right: 8 }}>
                <IconButton
                    aria-label={`${label}: ${collection.name}`}
                    disabled={!resolvedURL}
                    onClick={copy}
                    sx={{
                        width: 28,
                        height: 28,
                        p: 0,
                        borderRadius: "50%",
                        bgcolor: "#FFFFFF",
                        color: "#161616",
                        boxShadow: "0px 2px 6px rgba(0,0,0,0.15)",
                        "&:hover": { bgcolor: "#F4F4F4" },
                    }}
                >
                    <HugeiconsIcon
                        icon={copied ? Tick02Icon : Copy01Icon}
                        size={12}
                        strokeWidth={1.5}
                    />
                </IconButton>
            </span>
        </Tooltip>
    );
};
