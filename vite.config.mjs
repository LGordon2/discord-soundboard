import { defineConfig } from 'vite';
import { minify } from 'html-minifier-terser';

export default defineConfig({
    plugins: [{
        name: 'minify-output-html',
        apply: 'build',
        enforce: 'post',
        async generateBundle(_options, bundle) {
            for (const asset of Object.values(bundle)) {
                if (asset.type === 'asset' && asset.fileName.endsWith('.html')) {
                    asset.source = await minify(String(asset.source), {
                        collapseWhitespace: true,
                        removeComments: true,
                        removeRedundantAttributes: true,
                        removeScriptTypeAttributes: true,
                        removeStyleLinkTypeAttributes: true,
                    });
                }
            }
        },
    }],
});
