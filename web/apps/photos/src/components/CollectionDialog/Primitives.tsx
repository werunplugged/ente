import { Search01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import CloseIcon from "@mui/icons-material/Close";
import { InputBase, Stack, type SxProps, type Theme } from "@mui/material";
import { isSxArray } from "ente-base/components/utils/sx";
import { t } from "i18next";
import React, { useRef } from "react";

export const CollectionDialogSearchField: React.FC<{
    sx?: SxProps<Theme>;
    placeholder?: string;
    value: string;
    onChange: (value: string) => void;
}> = ({ value, onChange, sx = [], placeholder = t("albums_search_hint") }) => {
    const inputRef = useRef<HTMLInputElement>(null);

    const handleClear = () => {
        onChange("");
        inputRef.current?.focus();
    };

    return (
        <Stack
            direction="row"
            sx={[searchFieldSx, ...(isSxArray(sx) ? sx : [sx])]}
        >
            <Stack sx={searchIconSx}>
                <HugeiconsIcon
                    icon={Search01Icon}
                    size={18}
                    color="currentColor"
                />
            </Stack>
            <InputBase
                inputRef={inputRef}
                fullWidth
                autoFocus
                placeholder={placeholder}
                value={value}
                onChange={(e) => onChange(e.target.value)}
                sx={searchInputSx}
                endAdornment={
                    value ? (
                        <CloseIcon
                            fontSize="small"
                            onClick={handleClear}
                            sx={clearIconSx}
                        />
                    ) : undefined
                }
            />
        </Stack>
    );
};

const searchIconSx = (theme: Theme) => ({
    flexShrink: 0,
    color: "rgba(0 0 0 / 0.4)",
    ...theme.applyStyles("dark", { color: "rgba(255 255 255 / 0.4)" }),
});

const searchFieldSx = (theme: Theme) => ({
    alignItems: "center",
    gap: "10px",
    height: 44,
    borderRadius: "16px",
    backgroundColor: "background.paper",
    px: "14px",
    ...theme.applyStyles("dark", { backgroundColor: "#282828" }),
});

const searchInputSx = {
    fontSize: 14,
    lineHeight: "20px",
    fontWeight: 500,
    color: "text.base",
    "& input::placeholder": { color: "text.muted", opacity: 1 },
};
const clearIconSx = {
    color: "stroke.muted",
    cursor: "pointer",
    "&:hover": { color: "text.base" },
};
